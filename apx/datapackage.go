package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

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

type DataPackageMessage struct {
	Cmd  MessageType       `json:"cmd"`
	Data DataPackageObject `json:"data"`
}

type DataPackageObject struct {
	Games map[string]GameData `json:"games"`
}

type datapackageCache struct {
	mu       sync.RWMutex
	packages map[string]GameData // keyed by game name
	fetchSem chan struct{}       // semaphore: buffer of 1
}

func newDatapackageCache() *datapackageCache {
	return &datapackageCache{
		packages: make(map[string]GameData),
		fetchSem: make(chan struct{}, 1),
	}
}

func (dc *datapackageCache) Get(game string) (GameData, bool) {
	dc.mu.RLock()
	defer dc.mu.RUnlock()
	gd, ok := dc.packages[game]
	return gd, ok
}

func (dc *datapackageCache) Set(game string, gd GameData) {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	dc.packages[game] = gd
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

	games := make([]string, 0)
	// If no games specified, use all games
	if len(msg.Games) == 0 {
		for game := range s.roomInfo.DatapackageChecksums {
			games = append(games, game)
		}
	} else {
		// Only include games we know exist from the roominfo datapackage checksums map
		for _, game := range msg.Games {
			if _, ok := s.roomInfo.DatapackageChecksums[game]; ok {
				games = append(games, game)
			}
		}
	}

	if !connState.authenticated {
		// Don't send datapackages until after authed
		connState.pendingDatapackGames = append(connState.pendingDatapackGames, games...)
		return nil
	}

	return s.sendDataPackages(ctx, connState.clientConn, games)
}

func (s apxServer) fetchDataPackageFromAPServer(ctx context.Context, game string) (GameData, error) {
	// For the sake of being local, we'll just ignore checksums for now

	// Semaphore to only fetch 1 at a time, prevent issues with max packet size
	select {
	case s.datapackages.fetchSem <- struct{}{}:
	case <-ctx.Done():
		return GameData{}, ctx.Err()
	}
	defer func() { <-s.datapackages.fetchSem }()

	// Check cache again after acquiring semaphore (another client may have called a goroutine that's fetched it)
	if gd, ok := s.datapackages.Get(game); ok {
		return gd, nil
	}

	apConn, _, err := websocket.Dial(ctx, fmt.Sprintf("ws://%s:%d", s.config.APHost, s.config.APPort), nil)
	if err != nil {
		return GameData{}, fmt.Errorf("dialing upstream: %w", err)
	}
	defer apConn.CloseNow()
	apConn.SetReadLimit(1 << 24)

	// We don't actually care about the initial message, we just do basic validation here
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
		return GameData{}, fmt.Errorf("reading DataPackage response for '%s' (may be too large): %w", game, err)
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

func (s apxServer) sendDataPackages(ctx context.Context, client *websocket.Conn, games []string) error {
	result := DataPackageObject{Games: make(map[string]GameData)}

	for _, game := range games {
		gd, ok := s.datapackages.Get(game)
		if !ok {
			fetched, err := s.fetchDataPackageFromAPServer(ctx, game)
			if err != nil {
				return fmt.Errorf("fetching datapackage for %q: %w", game, err)
			}
			s.datapackages.Set(game, fetched)
			gd = fetched
		}
		result.Games[game] = gd
	}

	resp := DataPackageMessage{
		Cmd:  MessageTypeDataPackage,
		Data: result,
	}
	return wsjson.Write(ctx, client, []any{resp})
}
