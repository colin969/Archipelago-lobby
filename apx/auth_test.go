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
