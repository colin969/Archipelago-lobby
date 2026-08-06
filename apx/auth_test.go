package main

import (
	"encoding/json"
	"testing"
)

func TestReducedMessages(t *testing.T) {
	slot := 3
	other := 5

	cases := []struct {
		name   string
		msg    any
		expect bool
	}{
		{"non-PrintJSON always forwarded", map[string]any{"cmd": "ReceivedItems"}, true},
		{"unknown PrintJSON type forwarded", map[string]any{"cmd": "PrintJSON", "type": "Chat"}, true},
		{"ItemSend to this slot", map[string]any{"cmd": "PrintJSON", "type": "ItemSend", "slot": &slot}, true},
		{"ItemSend to other slot", map[string]any{"cmd": "PrintJSON", "type": "ItemSend", "slot": &other}, false},
		{"ItemSend receiving this slot", map[string]any{"cmd": "PrintJSON", "type": "ItemSend", "receiving": &slot}, true},
		{"ItemSend not receiving this slot", map[string]any{"cmd": "PrintJSON", "type": "ItemSend", "receiving": &other}, false},
		{"Join with no slot info", map[string]any{"cmd": "PrintJSON", "type": "Join"}, false},
		{"Join to this slot", map[string]any{"cmd": "PrintJSON", "type": "Join", "slot": &slot}, true},
		{"invalid json not forwarded", nil, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var raw json.RawMessage
			if tc.msg == nil {
				raw = json.RawMessage(`{invalid}`)
			} else {
				var err error
				raw, err = json.Marshal(tc.msg)
				if err != nil {
					t.Fatalf("marshal: %v", err)
				}
			}

			if got := allowReducedMessage(raw, slot); got != tc.expect {
				t.Errorf("shouldForwardMessage() = %v, want %v", got, tc.expect)
			}
		})
	}
}

var sampleMessages = func() []json.RawMessage {
	msgs := []map[string]any{
		{"cmd": "PrintJSON", "type": "ItemSend", "receiving": 1, "slot": 2, "data": []any{"some", "item", "data"}},
		{"cmd": "PrintJSON", "type": "Join", "receiving": 1, "slot": 1, "data": []any{"player joined"}},
		{"cmd": "RoomUpdate", "players": []any{"a", "b", "c"}},
		{"cmd": "PrintJSON", "type": "Hint", "receiving": 3, "slot": 1, "data": []any{"hint data"}},
		{"cmd": "DataPackage", "data": map[string]any{"games": map[string]any{}}},
	}
	raw := make([]json.RawMessage, len(msgs))
	for i, m := range msgs {
		raw[i], _ = json.Marshal(m)
	}
	return raw
}()

// Benchmark for overhead on reduced client message filtering
func BenchmarkReducedFilterCycle(b *testing.B) {
	slotId := 1
	// Pre-encode a batch as if received from AP
	batch, _ := json.Marshal(sampleMessages)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var response []json.RawMessage
		if err := json.Unmarshal(batch, &response); err != nil {
			b.Fatal(err)
		}
		filtered := response[:0]
		for _, raw := range response {
			if allowReducedMessage(raw, slotId) {
				filtered = append(filtered, raw)
			}
		}
		// Simulate the wsjson.Write marshal step
		if len(filtered) > 0 {
			if _, err := json.Marshal(filtered); err != nil {
				b.Fatal(err)
			}
		}
	}
}
