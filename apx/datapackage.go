package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type GameData struct {
	ItemNameToID     map[string]int64 `json:"item_name_to_id"`
	LocationNameToID map[string]int64 `json:"location_name_to_id"`
	Checksum         string           `json:"checksum"`
}

type GetDataPackageMessage struct {
	Cmd   MessageType `json:"cmd"`
	Games []string    `json:"games"`
}

type EncodedDataPackageMessage struct {
	Cmd  MessageType              `json:"cmd"`
	Data EncodedDataPackageObject `json:"data"`
}

type DataPackageMessage struct {
	Cmd  MessageType       `json:"cmd"`
	Data DataPackageObject `json:"data"`
}

type DataPackageObject struct {
	Games map[string]GameData `json:"games"`
}

type EncodedDataPackageObject struct {
	Games map[string]json.RawMessage `json:"games"`
}

// Immutable
type DataPackageStore struct {
	singleRepOptimization bool                        // Allow single game requests caching, doubles memory usage
	packages              map[string]json.RawMessage  // Raw encoded datapackages keyed by game name
	singleResponses       map[string]json.RawMessage  // Pre-built response for single-game requests
	ItemIDToName          map[string]map[int64]string // game -> id -> name
	LocationIDToName      map[string]map[int64]string // game -> id -> name
}

func newDataPackageStore(singleRepOptimization bool) *DataPackageStore {
	return &DataPackageStore{
		singleRepOptimization: singleRepOptimization,
		packages:              make(map[string]json.RawMessage),
		singleResponses:       make(map[string]json.RawMessage),
		ItemIDToName:          make(map[string]map[int64]string),
		LocationIDToName:      make(map[string]map[int64]string),
	}
}

func (ds *DataPackageStore) AddDataPackage(game string, gd GameData) error {
	encodedData, err := json.Marshal(gd)
	if err != nil {
		return err
	}
	ds.packages[game] = encodedData

	if ds.singleRepOptimization {
		encodedKey, _ := json.Marshal(game)
		msg := []byte(`[{"cmd":"DataPackage","data":{"games":{`)
		msg = append(msg, encodedKey...)
		msg = append(msg, ':')
		msg = append(msg, encodedData...)
		msg = append(msg, `}}}]`...)
		ds.singleResponses[game] = msg
	}

	// Populate global item id to name
	itemIDToName := make(map[int64]string, len(gd.ItemNameToID))
	for name, id := range gd.ItemNameToID {
		itemIDToName[id] = name
	}
	ds.ItemIDToName[game] = itemIDToName

	// Populate global location id to name
	locationIDToName := make(map[int64]string, len(gd.LocationNameToID))
	for name, id := range gd.LocationNameToID {
		locationIDToName[id] = name
	}
	ds.LocationIDToName[game] = locationIDToName

	return nil
}

// MUST be called before server is live to other users. CANNOT be called safely after.
func (s apxServer) prefetchDataPackages(ctx context.Context) error {
	for game := range s.roomInfo.DatapackageChecksums {
		if _, ok := s.datapackages.packages[game]; ok {
			continue // already cached
		}
		gd, err := s.fetchDataPackageFromAPServer(ctx, game)
		if err != nil {
			return fmt.Errorf("prefetching datapackage for %q: %w", game, err)
		}
		s.datapackages.AddDataPackage(game, gd)
	}
	return nil
}

func (s apxServer) handleGetDataPackage(ctx context.Context, connState *connectionState, raw map[string]any) error {
	data, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("marshalling GetDataPackage: %w", err)
	}
	var msg GetDataPackageMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return fmt.Errorf("unmarshalling GetDataPackage: %w", err)
	}

	if len(msg.Games) == 0 {
		// Client requested all games, send them all together
		games := make([]string, 0, len(s.roomInfo.DatapackageChecksums))
		for game := range s.roomInfo.DatapackageChecksums {
			games = append(games, game)
		}
		return s.sendDataPackages(ctx, connState.clientConn, games)
	} else {
		// Good client requesting only some at a time
		// Apclientpp and other clients can fail if we try and send back in multiple messages, so don't try it
		games := make([]string, 0)
		for _, game := range msg.Games {
			if _, ok := s.roomInfo.DatapackageChecksums[game]; ok {
				games = append(games, game)
			}
		}
		return s.sendDataPackages(ctx, connState.clientConn, games)
	}

	// We can uncomment this if we want to delay datapackages again later. Need to do before the branches above, extra changes still.

	// if !connState.authenticated {
	// 	// Don't send datapackages until after authed
	// 	connState.pendingDatapackGames = append(connState.pendingDatapackGames, games...)
	// 	return nil
	// }
}

// Grab datapackage from AP server and cache locally so we can provide it to clients ourselves
func (s apxServer) fetchDataPackageFromAPServer(ctx context.Context, game string) (GameData, error) {
	apConn, _, err := websocket.Dial(ctx, fmt.Sprintf("ws://%s:%d", s.config.APHost, s.config.APPort), nil)
	if err != nil {
		return GameData{}, fmt.Errorf("dialing upstream: %w", err)
	}
	defer apConn.CloseNow()
	apConn.SetReadLimit(1 << 24)

	var roomInfo []map[string]any
	if err := wsjson.Read(ctx, apConn, &roomInfo); err != nil {
		return GameData{}, fmt.Errorf("reading RoomInfo: %w", err)
	}

	req := GetDataPackageMessage{Cmd: MessageTypeGetDataPackage, Games: []string{game}}
	if err := wsjson.Write(ctx, apConn, []any{req}); err != nil {
		return GameData{}, fmt.Errorf("sending GetDataPackage: %w", err)
	}

	apConn.SetReadLimit(wsReadLimit)

	var responses []map[string]any
	if err := wsjson.Read(ctx, apConn, &responses); err != nil {
		return GameData{}, fmt.Errorf("reading DataPackage response for %q (may be too large): %w", game, err)
	}

	for _, resp := range responses {
		if cmd, _ := resp["cmd"].(string); cmd != string(MessageTypeDataPackage) {
			continue
		}
		raw, err := json.Marshal(resp)
		if err != nil {
			return GameData{}, fmt.Errorf("marshalling DataPackage: %w", err)
		}
		var pkg DataPackageMessage
		if err := json.Unmarshal(raw, &pkg); err != nil {
			return GameData{}, fmt.Errorf("unmarshalling DataPackage: %w", err)
		}
		if gd, ok := pkg.Data.Games[game]; ok {
			return gd, nil
		}
	}

	return GameData{}, fmt.Errorf("DataPackage response did not contain game %q", game)
}

// Stitch together to avoid doing any json ops on the already encoded datapackage
func (s apxServer) sendDataPackages(ctx context.Context, client *websocket.Conn, games []string) error {
	if s.datapackages.singleRepOptimization && len(games) == 1 {
		raw, ok := s.datapackages.singleResponses[games[0]]
		if !ok {
			return fmt.Errorf("unknown datapackage for %q", games[0])
		}
		return client.Write(ctx, websocket.MessageText, raw)
	}

	msg := []byte(`[{"cmd":"DataPackage","data":{"games":{`)

	for i, game := range games {
		raw, ok := s.datapackages.packages[game]
		if !ok {
			return fmt.Errorf("unknown datapackage for %q", game)
		}
		if i > 0 {
			msg = append(msg, ',')
		}
		encodedKey, _ := json.Marshal(game)
		msg = append(msg, encodedKey...)
		msg = append(msg, ':')
		msg = append(msg, raw...)
	}

	msg = append(msg, `}}}]`...)
	return client.Write(ctx, websocket.MessageText, msg)
}
