use std::{
  collections::HashMap, env, io::Error as IoError, net::SocketAddr, sync::{Arc, Mutex}, time::SystemTime,
};

use serde::{Deserialize, Serialize};
use ustr::{UstrMap, UstrSet};

use futures_channel::mpsc::{unbounded, UnboundedSender};
use futures_util::{SinkExt, StreamExt, stream::TryStreamExt};

use tokio::{net::{TcpListener, TcpStream}, sync::RwLock};
use tokio_tungstenite::tungstenite::{connect, protocol::Message};

type Tx = UnboundedSender<Message>;
type PeerMap = Arc<Mutex<HashMap<SocketAddr, Tx>>>;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct NetworkVersion {
    pub major: u16,
    pub minor: u16,
    pub build: u16,
    pub class: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MsgRoomInfo {
    pub version: NetworkVersion,
    pub generator_version: NetworkVersion,
    pub tags: UstrSet,
    pub password: bool,
    pub permissions: HashMap<String, u8>,
    pub hint_cost: u8,
    pub location_check_points: u64,
    pub games: UstrSet,
    pub datapackage_checksums: UstrMap<String>,
    pub seed_name: String,
    pub time: f64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MsgGetDataPackage {
    pub games: Option<Vec<String>>
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MsgConnect {
    pub password: Option<String>,
    pub game: String,
    pub name: String,
    pub uuid: String,
    pub version: NetworkVersion,
    pub items_handling: u8,
    pub tags: UstrSet,
    pub slot_data: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MsgConnectionRefused {
    pub errors: Vec<String>,
}

#[derive(Debug, Serialize, Deserialize)]
#[serde(tag = "cmd")]
pub enum ApPacket {
    RoomInfo(MsgRoomInfo),
    GetDataPackage(MsgGetDataPackage),
    Connect(MsgConnect),
    ConnectionRefused(MsgConnectionRefused)
}

pub type ApPackets = Vec<ApPacket>;

async fn handle_connection(
    peer_map: PeerMap, 
    raw_stream: TcpStream, 
    addr: SocketAddr,
    ap_host: String,
    room_info: Option<MsgRoomInfo>,
    slot_passwords: Arc<RwLock<HashMap<String, String>>>
) {
    println!("Incoming TCP connection from: {}", addr);

    let ws_stream = tokio_tungstenite::accept_async(raw_stream)
        .await
        .expect("Error during the websocket handshake occurred");
    println!("WebSocket connection established: {}", addr);

    // Insert the write part of this peer to the peer map.
    let (tx, _) = unbounded();
    peer_map.lock().unwrap().insert(addr, tx);

    let (mut outgoing, mut incoming) = ws_stream.split();

    match room_info {
        Some(room_info_cache) => {
            let mut room_info = room_info_cache.clone();
            room_info.time = SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap()
                .as_secs_f64();

            let json = serde_json::to_string(&vec![ApPacket::RoomInfo(room_info)]).unwrap();
            outgoing.send(Message::Text(json.into())).await.ok();
        }
        None => {
            outgoing.close().await.ok();
            peer_map.lock().unwrap().remove(&addr);
            return;
        }
    }

    loop {
        let msg = match incoming.try_next().await {
            Ok(Some(msg)) => msg,
            _ => {
                peer_map.lock().unwrap().remove(&addr);
                return;
            }
        };

        println!("[{addr}] <- client (pre-auth): {msg}");

        let Ok(text) = msg.to_text() else { continue };
        let Ok(packets) = serde_json::from_str::<ApPackets>(text) else { continue };

        for packet in packets {
            match packet {
                ApPacket::Connect(mut connect_msg) => {
                    proxy_connection(ap_host.clone(), connect_msg, addr,&mut outgoing, &mut incoming).await;

                    // let passwords = slot_passwords.read().await;
                    // match passwords.get(&connect_msg.name) {
                    //     None => {
                    //         // No password assigned to slot name
                    //         println!("Bad slot");
                    //         let refused_msg = MsgConnectionRefused {
                    //             errors: vec!["InvalidSlot".to_owned()]
                    //         };
                    //         let json_msg = serde_json::to_string(&vec![ApPacket::ConnectionRefused(refused_msg)]).unwrap();
                    //         outgoing.send(Message::Text(json_msg.into())).await.ok();
                    //     },
                    //     Some(slot_password) => {
                    //         // Password found for slot name, now compare to given password
                    //         println!("Slot found, checking password");
                    //         let provided = connect_msg.password.as_deref().unwrap_or("");
                    //         if provided != slot_password {
                    //             println!("Bad password");
                    //             let refused_msg = MsgConnectionRefused {
                    //                 errors: vec!["InvalidPassword".to_owned()]
                    //             };
                    //             let json_msg = serde_json::to_string(&vec![ApPacket::ConnectionRefused(refused_msg)]).unwrap();
                    //             outgoing.send(Message::Text(json_msg.into())).await.ok();
                    //         } else {
                    //             println!("Proxying...");
                    //             connect_msg.password = None;
                    //             // Allow access to server, forward on the message and just act as a proxy. Another function maybe?
                    //             proxy_connection(ap_host.clone(), connect_msg, addr,&mut outgoing, &mut incoming).await;
                    //             return;
                    //         }
                    //     }
                    // }
                }
                ApPacket::GetDataPackage(gdp_msg) => {
                    // TODO
                }
                _ => {
                    peer_map.lock().unwrap().remove(&addr);
                    return;
                }
            }
        }
    };
}

async fn proxy_connection(
    ap_host: String,
    connect_msg: MsgConnect,
    addr: SocketAddr,
    outgoing: &mut futures_util::stream::SplitSink<tokio_tungstenite::WebSocketStream<TcpStream>, Message>,
    incoming: &mut futures_util::stream::SplitStream<tokio_tungstenite::WebSocketStream<TcpStream>>,
) {
    println!("[{addr}] Connecting client to AP server");

    // Open a connection to the AP server
    let ap_stream = match tokio_tungstenite::connect_async(&ap_host).await {
        Ok((ws, _)) => ws,
        Err(e) => {
            eprintln!("Failed to connect to AP server: {e}");
            return;
        }
    };

    let (mut ap_out, mut ap_in) = ap_stream.split();

    // Forward the original Connect message
    let connect_json = serde_json::to_string(&vec![ApPacket::Connect(connect_msg)]).unwrap();
    ap_out.send(Message::Text(connect_json.into())).await.ok();

    // Proxy both incoming and outgoing traffic to the AP server
    loop {
        tokio::select! {
            client_msg = incoming.try_next() => {
                match client_msg {
                    Ok(Some(msg)) => { ap_out.send(msg).await.ok(); }
                    _ => break,
                }
            }
            ap_msg = ap_in.try_next() => {
                match ap_msg {
                    Ok(Some(msg)) => { outgoing.send(msg).await.ok(); }
                    _ => break,
                }
            }
        }
    }
}


// Cache room info so we can respond to initial connections without asking the AP server each time
async fn get_room_info(
    ap_host: String,
    room_info_lock: Arc<RwLock<Option<MsgRoomInfo>>>,
) -> anyhow::Result<String> {
    let (mut ws_stream, _) = connect(&ap_host)?;
    
    let raw_room_info = ws_stream.read()?;
    let text = raw_room_info.to_text()?.to_owned();

    let packets: ApPackets = serde_json::from_str(&text)?;
    let packet = packets.into_iter().next().ok_or_else(|| anyhow::anyhow!("Empty packet list"))?;
    let ApPacket::RoomInfo(mut room_info) = packet else {
        return Err(anyhow::anyhow!("Expected RoomInfo as first packet"));
    };
    // Set password to true so that clients will send per-slot passwords later
    // room_info.password = true;
    println!("Connected to AP server and gotten RoomInfo: {}", serde_json::to_string(&room_info)?);
    *room_info_lock.write().await = Some(room_info);
    

    Ok(raw_room_info.to_text().unwrap().to_owned())
}

#[tokio::main]
async fn main() -> Result<(), IoError> {
    let port = env::args().nth(1)
        .or_else(|| env::var("PORT").ok())
        .unwrap_or_else(|| "9090".to_string());

    let addr = format!("0.0.0.0:{}", port);

    let ap_room_host = std::env::var("AP_ROOM_HOST").expect("Provide an `AP_ROOM_HOST` env variable");
    let ap_port = env::var("AP_ROOM_PORT")
        .expect("Provide an `AP_ROOM_PORT` env variable")
        .parse::<u16>()
        .expect("AP_ROOM_PORT must be a valid port number");
    let conn_str = format!("ws://{}:{}", ap_room_host, ap_port);
    
    let password_map = HashMap::from([
        ("Colin_NineSols".to_string(), "password".to_string()),
        ("Colin_Payday".to_string(), "1234".to_string()),
    ]);
    let slot_passwords: Arc<RwLock<HashMap<String, String>>> = Arc::new(RwLock::new(password_map));
    
    let room_info: Arc<RwLock<Option<MsgRoomInfo>>> = Arc::new(RwLock::new(None));
    get_room_info(conn_str.clone(), Arc::clone(&room_info)).await.expect("Failed to connect to AP server");

    let state = PeerMap::new(Mutex::new(HashMap::new()));

    // Create the event loop and TCP listener we'll accept connections on.
    let try_socket = TcpListener::bind(&addr).await;
    let listener = try_socket.expect("Failed to bind");
    println!("Listening on: {}", addr);

    // Let's spawn the handling of each connection in a separate task.
    while let Ok((stream, addr)) = listener.accept().await {
        let room_info_cache = room_info.read().await.clone();
        tokio::spawn(handle_connection(state.clone(), stream, addr, conn_str.clone(), room_info_cache, Arc::clone(&slot_passwords)));
    }

    Ok(())
}