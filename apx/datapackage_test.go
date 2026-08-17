package main

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

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
