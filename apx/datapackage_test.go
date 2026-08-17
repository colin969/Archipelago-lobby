package main

import (
	"encoding/json"
	"os"
	"testing"
)

// Does not cover single game optimizations, since that has nothing except 2 compares
func BenchmarkSendDataPackages(b *testing.B) {
	testDataPackageJSON, err := os.ReadFile("testdata/datapackage.json")
	if err != nil {
		panic(err)
	}

	ds := newDataPackageStore(false)
	ds.packages["TestGame"] = json.RawMessage(testDataPackageJSON)

	s := apxServer{
		datapackages: ds,
	}

	for b.Loop() {
		encodedKey, _ := json.Marshal("TestGame")
		msg := []byte(`[{"cmd":"DataPackage","data":{"games":{`)
		msg = append(msg, '"')
		msg = append(msg, encodedKey...)
		msg = append(msg, '"', ':')
		msg = append(msg, s.datapackages.packages["TestGame"]...)
		msg = append(msg, `}}}]`...)

		_ = msg // replace with client.Write in real usage
	}
}
