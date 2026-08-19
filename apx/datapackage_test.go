package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

func TestRetryStormGameKeyCache(t *testing.T) {
	connState := &connectionState{}

	raw := map[string]any{
		"cmd":   "GetDataPackage",
		"games": []any{"Archipelago", "Clique"},
	}

	s := apxServer{
		roomInfo: RoomInfoMessage{
			DatapackageChecksums: map[string]string{
				"Archipelago": "abc",
				"Clique":      "def",
				"Celeste":     "ghi",
			},
		},
		datapackages: newDataPackageStore(true),
	}

	// First req - should set key
	_ = s.handleGetDataPackage(context.Background(), connState, raw)
	if connState.prevDatapackageGamesReq == "" {
		t.Fatal("expected key to be set")
	}

	// Second req - identical request, should be same key
	prev := connState.prevDatapackageGamesReq
	_ = s.handleGetDataPackage(context.Background(), connState, raw)
	if connState.prevDatapackageGamesReq != prev {
		t.Fatal("expected key to be unchanged on retry")
	}

	// Different order - should still be the same key because of sorting
	raw2 := map[string]any{
		"cmd":   "GetDataPackage",
		"games": []any{"Clique", "Archipelago"},
	}
	_ = s.handleGetDataPackage(context.Background(), connState, raw2)
	if connState.prevDatapackageGamesReq != prev {
		t.Fatal("expected sort to normalize order")
	}

	// Different games - should be different key
	raw3 := map[string]any{
		"cmd":   "GetDataPackage",
		"games": []any{"Clique", "Archipelago", "Celeste"},
	}
	_ = s.handleGetDataPackage(context.Background(), connState, raw3)
	if connState.prevDatapackageGamesReq == prev {
		t.Fatal("expected key to change for new games")
	}
}

// Does not cover single game optimizations, since that has nothing except 2 compares
func BenchmarkSendDataPackages(b *testing.B) {
	testDataPackageJSON, err := os.ReadFile("testdata/datapackage.json")
	if err != nil {
		panic(err)
	}

	// Vague simulation of 20 games
	ds := newDataPackageStore(false)
	games := make([]string, 20)
	for i := range 20 {
		name := fmt.Sprintf("TestGame%d", i)
		ds.packages[name] = json.RawMessage(testDataPackageJSON)
		encodedKey, _ := json.Marshal(name)
		ds.encodedGameNameKeys[name] = encodedKey
		games[i] = name
	}

	s := apxServer{
		datapackages: ds,
	}

	const header = `[{"cmd":"DataPackage","data":{"games":{`
	const footer = `}}}]`
	for b.Loop() {
		size := len(header) + len(footer) + len(games) - 1
		for _, game := range games {
			size += len(s.datapackages.encodedGameNameKeys[game]) + 1 + len(s.datapackages.packages[game])
		}
		msg := make([]byte, 0, size)
		msg = append(msg, header...)
		for i, game := range games {
			if i > 0 {
				msg = append(msg, ',')
			}
			msg = append(msg, s.datapackages.encodedGameNameKeys[game]...)
			msg = append(msg, ':')
			msg = append(msg, s.datapackages.packages[game]...)
		}
		msg = append(msg, footer...)
	}
}
