package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// Make sure only the right bounce messages reach the right clients
func TestBroadcastBounce(t *testing.T) {
	game := "Celeste"
	game2 := "Hollow Knight"

	cases := []struct {
		name          string
		clientTags    []string
		clientSlot    int
		clientGame    string
		msgTags       []string
		msgSlots      []int
		msgGames      []string
		expectReceive bool
	}{
		// Tag matching
		{"tag match", []string{"DeathLink", "AP"}, 1, game, []string{"DeathLink"}, nil, nil, true},
		{"tag no match", []string{"AP"}, 1, game, []string{"DeathLink"}, nil, nil, false},

		// Slot matching
		{"slot match", nil, 3, game, nil, []int{3}, nil, true},
		{"slot no match", nil, 3, game, nil, []int{1, 2}, nil, false},

		// Game matching
		{"game match", nil, 1, game, nil, nil, []string{game}, true},
		{"game no match", nil, 1, game, nil, nil, []string{game2}, false},

		// Weird packet just for the sake of it
		{"all empty", []string{"AP"}, 3, game, nil, nil, nil, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn, received := newTestWSClient(t)

			rc := &registeredClient{
				slotId:     tc.clientSlot,
				game:       &tc.clientGame,
				clientConn: conn,
				cancel:     func() {},
				reduced:    false,
			}

			reg := newConnectionRegistry()
			reg.Register(tc.clientSlot, rc, tc.clientTags)

			reg.BroadcastBounce(context.Background(), BounceMessage{
				Tags:  tc.msgTags,
				Slots: tc.msgSlots,
				Games: tc.msgGames,
				Data:  map[string]any{"test": true},
			})

			select {
			case <-received:
				if !tc.expectReceive {
					t.Error("client received message but should not have")
				}
			case <-time.After(200 * time.Millisecond):
				if tc.expectReceive {
					t.Error("client did not receive message but should have")
				}
			}
		})
	}
}

func TestBroadcastBounce_SlotExclusions(t *testing.T) {
	game := "Celeste"

	cases := []struct {
		name          string
		clientTags    []string
		msgTags       []string
		excludedTags  []string // tags excluded for the sending slot
		expectReceive bool
	}{
		{"all tags excluded", []string{"DeathLink"}, []string{"DeathLink"}, []string{"DeathLink"}, false},
		{"some matching tags survive exclusion", []string{"DeathLink", "AP"}, []string{"DeathLink", "AP"}, []string{"DeathLink"}, true},
		{"some non-matching tags survive exclusion", []string{"DeathLink"}, []string{"DeathLink", "AP"}, []string{"DeathLink"}, false},
		{"excluded tag not in message", []string{"DeathLink"}, []string{"DeathLink"}, []string{"AP"}, true},
		{"no exclusions", []string{"DeathLink"}, []string{"DeathLink"}, nil, true},
		{"no tags in message", []string{"DeathLink", "AP"}, []string{}, []string{"DeathLink"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn, received := newTestWSClient(t)

			rc := &registeredClient{
				slotId:     1,
				game:       &game,
				clientConn: conn,
				cancel:     func() {},
				reduced:    false,
			}

			reg := newConnectionRegistry()
			reg.Register(1, rc, tc.clientTags)

			// Simulate BroadcastBounceFromSlot manually to avoid needing a bounce info store for testing
			excludedSet := make(map[string]struct{}, len(tc.excludedTags))
			for _, tag := range tc.excludedTags {
				excludedSet[tag] = struct{}{}
			}

			msg := BounceMessage{
				Tags: slices.DeleteFunc(slices.Clone(tc.msgTags), func(tag string) bool {
					_, excluded := excludedSet[tag]
					return excluded
				}),
				Data: map[string]any{"test": true},
			}

			reg.BroadcastBounce(context.Background(), msg)

			select {
			case <-received:
				if !tc.expectReceive {
					t.Errorf("client received message but should not have (tags after exclusion: %v)", msg.Tags)
				}
			case <-time.After(200 * time.Millisecond):
				if tc.expectReceive {
					t.Errorf("client did not receive message but should have (tags after exclusion: %v)", msg.Tags)
				}
			}
		})
	}
}

func newTestWSClient(t *testing.T) (*websocket.Conn, <-chan struct{}) {
	t.Helper()
	received := make(chan struct{}, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		var msg []any
		if wsjson.Read(r.Context(), conn, &msg) == nil {
			received <- struct{}{}
		}
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dial test ws client: %v", err)
	}

	return conn, received
}
