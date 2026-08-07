package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type CmdPeek struct {
	Cmd string `json:"cmd"`
}

type PrintJSONPeek struct {
	Receiving *int   `json:"receiving"`
	Slot      *int   `json:"slot"`
	Type      string `json:"type"`
}

func (s apxServer) handleConnect(ctx context.Context, connState *connectionState, raw map[string]any) error {
	data, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("marshalling connect message: %w", err)
	}

	var msg ConnectMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return fmt.Errorf("unmarshalling connect message: %w", err)
	}

	log.Printf("connect: game=%q name=%q uuid=%q version=%+v tags=%v slotData=%v",
		msg.Game, msg.Name, msg.UUID, msg.Version, msg.Tags, msg.SlotData)

	connState.slotName = &msg.Name

	// Lobby enabled, enforce lobby passwords
	if s.config.LobbyEnabled {
		slotEntry, ok := s.roomPlayers.auth[msg.Name]
		if !ok {
			errorMsg := ConnectionRefusedMessage{
				Cmd:    "ConnectionRefused",
				Errors: []string{"InvalidSlot"},
			}
			s.logf("InvalidSlot for %s", msg.Name)
			return wsjson.Write(ctx, connState.clientConn, []any{errorMsg})
		}
		password, ok := s.passwords.Get(slotEntry[1])
		if !(ok && msg.Password != nil && password == *msg.Password) {
			errorMsg := ConnectionRefusedMessage{
				Cmd:    "ConnectionRefused",
				Errors: []string{"InvalidPassword"},
			}
			s.logf("InvalidPassword for %s", msg.Name)
			return wsjson.Write(ctx, connState.clientConn, []any{errorMsg})
		}
	}

	connState.authenticated = true

	if len(connState.pendingDatapackGames) > 0 {
		if err := s.sendDataPackages(ctx, connState.clientConn, connState.pendingDatapackGames); err != nil {
			s.logf("error sending pending datapackages: %v", err)
		}
		connState.pendingDatapackGames = nil
	}

	apConn, slotId, game, err := s.connectAP(ctx, connState.clientConn, connState.reduced, msg)
	if err != nil {
		return fmt.Errorf("connecting to AP: %w", err)
	}
	connState.apConn = apConn
	client := registeredClient{
		slotId:     slotId,
		game:       game,
		cancel:     connState.cancel,
		clientConn: connState.clientConn,
	}
	s.connections.Register(slotId, &client, msg.Tags)
	connState.registeredClient = &client

	log.Printf("Connected to %s", msg.Name)

	return nil
}

func (s apxServer) handleConnectUpdate(ctx context.Context, connState *connectionState, raw map[string]any) error {
	data, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("marshalling connectupdate message: %w", err)
	}

	var msg ConnectUpdateMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return fmt.Errorf("unmarshalling connectupdate message: %w", err)
	}

	s.connections.UpdateTags(connState.registeredClient, msg.Tags)

	if connState.apConn != nil {
		return wsjson.Write(ctx, connState.apConn, []any{raw})
	}
	return nil
}

func (s apxServer) connectAP(ctx context.Context, client *websocket.Conn, reduced bool, connectMsg ConnectMessage) (*websocket.Conn, int, *string, error) {
	// Fix password when talking to ap server (only needed when using per-slot passwords)
	if s.config.LobbyEnabled {
		if s.roomInfo.Password {
			connectMsg.Password = &s.config.APPassword
		} else {
			connectMsg.Password = nil
		}
	}

	log.Printf("Connecting to AP server at %s", fmt.Sprintf("ws://%s:%d", s.config.APHost, s.config.APPort))

	apConn, _, err := websocket.Dial(ctx, fmt.Sprintf("ws://%s:%d", s.config.APHost, s.config.APPort), nil)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("dialing AP server: %w", err)
	}

	apConn.SetReadLimit(wsReadLimit)

	// Wait for RoomInfo from AP server (just so we know it's ready to accept the Connect message)
	var roomInfo []map[string]any
	if err := wsjson.Read(ctx, apConn, &roomInfo); err != nil {
		apConn.CloseNow()
		return nil, 0, nil, fmt.Errorf("reading RoomInfo from AP: %w", err)
	}

	// Send connect message
	connectMsg.ReducedTraffic = reduced
	if err := wsjson.Write(ctx, apConn, []any{connectMsg}); err != nil {
		apConn.CloseNow()
		return nil, 0, nil, fmt.Errorf("forwarding Connect to AP: %w", err)
	}

	// Read Connected (or ConnectionRefused) response from AP
	var response []map[string]any
	if err := wsjson.Read(ctx, apConn, &response); err != nil {
		apConn.CloseNow()
		return nil, 0, nil, fmt.Errorf("reading Connected from AP: %w", err)
	}

	// Forward the Connected message to the client
	if err := wsjson.Write(ctx, client, response); err != nil {
		apConn.CloseNow()
		return nil, 0, nil, fmt.Errorf("forwarding Connected to client: %w", err)
	}

	slotId := 0
	game := ""
	for _, msg := range response {
		if cmd, ok := msg["cmd"].(string); ok && cmd == "Connected" {
			if slot, ok := msg["slot"].(float64); ok {
				slotId = int(slot)
			}
			if slotInfo, ok := msg["slot_info"].(map[string]any); ok {
				slotKey := fmt.Sprintf("%d", slotId)
				if slotData, ok := slotInfo[slotKey].(map[string]any); ok {
					if g, ok := slotData["game"].(string); ok {
						game = g
					}
				}
			}
		}
	}

	// Proxy AP -> client

	// Avoid any processing on these packets where possible

	// Allow 30 messages to be queued at a time. Maybe this is awful, unsure.
	// This should never include datapackages since we handle them ourselves!
	msgs := make(chan struct {
		msgType websocket.MessageType
		data    []byte
	}, 30)

	// Collect messages from server - We don't want to fail catching a ping from read if the client is slow
	go func() {
		defer apConn.CloseNow()
		defer close(msgs)
		for {
			msgType, data, err := apConn.Read(ctx)
			if err != nil {
				if !isNormalClose(err) && ctx.Err() == nil {
					s.logf("AP read error: %v", err)
				}
				return
			}
			select {
			// Space on channel, push there
			case msgs <- struct {
				msgType websocket.MessageType
				data    []byte
			}{msgType, data}:
			// Context asked us to exit
			case <-ctx.Done():
				return
			// Client has backed up a lot of messages!
			default:
				s.logf("client being too slow :(")
			}
		}
	}()

	// Send messages to client
	go func() {
		for msg := range msgs {
			if err := client.Write(ctx, msg.msgType, msg.data); err != nil {
				if ctx.Err() == nil {
					log.Printf("client write error: %v", err)
				}
				return
			}
		}
	}()

	return apConn, slotId, &game, nil
}

func isNormalClose(err error) bool {
	if err == nil {
		return true
	}
	status := websocket.CloseStatus(err)
	if status == websocket.StatusNormalClosure || status == websocket.StatusGoingAway {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "use of closed network connection") || strings.Contains(msg, "context canceled")
}
