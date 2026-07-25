package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

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

	authenticated := false
	if !s.config.LobbyEnabled {
		// No lobby, no password enforcement
		authenticated = true
	} else {
		password, ok := s.passwords.Get(*connState.slotName)
		if ok && msg.Password != nil && password == *msg.Password {
			authenticated = true
		}
	}

	if authenticated {
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
		s.connections.Register(*connState.slotName, &client, msg.Tags)
		connState.registeredClient = &client

		log.Printf("Connected to %s", msg.Name)
	} else {
		log.Printf("Bad password for %s", msg.Name)
	}

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

	log.Println("Waiting for roominfo")

	// Wait for RoomInfo from AP server (just so we know it's ready to accept the Connect message)
	var roomInfo []map[string]any
	if err := wsjson.Read(ctx, apConn, &roomInfo); err != nil {
		apConn.CloseNow()
		return nil, 0, nil, fmt.Errorf("reading RoomInfo from AP: %w", err)
	}

	log.Println("Forwarding connect")

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

	log.Println("Proxying")

	// Proxy AP -> client

	if reduced {
		// Reduced packets sent to client
		go func() {
			defer apConn.CloseNow()
			for {
				var response []json.RawMessage
				err := wsjson.Read(ctx, apConn, &response)
				if err != nil {
					if ctx.Err() == nil {
						log.Printf("AP read error: %v", err)
					}
					return
				}

				// Filter out PrintJSON messages which don't involve this slot
				filtered := response[:0] // Reuse array to avoid more allocation
				for _, raw := range response {
					if allowReducedMessage(raw, slotId) {
						filtered = append(filtered, raw)
					}
				}
				if len(filtered) > 0 {
					if err := wsjson.Write(ctx, client, filtered); err != nil {
						if ctx.Err() == nil {
							log.Printf("client write error: %v", err)
						}
						return
					}
				}
			}
		}()
	} else {
		// Keep non-reduced proxying as slim as possible
		go func() {
			defer apConn.CloseNow()
			for {
				msgType, data, err := apConn.Read(ctx)
				if err != nil {
					if ctx.Err() == nil {
						log.Printf("AP read error: %v", err)
					}
					return
				}
				if err := client.Write(ctx, msgType, data); err != nil {
					if ctx.Err() == nil {
						log.Printf("client write error: %v", err)
					}
					return
				}
			}
		}()
	}

	return apConn, slotId, &game, nil
}

func allowReducedMessage(raw json.RawMessage, slotId int) bool {
	var cmdPeek CmdPeek
	if err := json.Unmarshal(raw, &cmdPeek); err != nil {
		return false
	}

	if cmdPeek.Cmd != "PrintJSON" {
		return true
	}

	var peek PrintJSONPeek
	if err := json.Unmarshal(raw, &peek); err != nil {
		return false
	}

	switch peek.Type {
	case "Join", "Part", "TagsChanged", "ItemSend", "ItemCheat", "Hint":
		receiving := peek.Receiving != nil && *peek.Receiving == slotId
		slot := peek.Slot != nil && *peek.Slot == slotId
		return receiving || slot
	}

	return true
}
