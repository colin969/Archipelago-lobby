package main

import (
	"context"
	"net/http"
	"slices"
	"sync"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type passwordStore struct {
	mu        sync.RWMutex
	passwords map[int]string
}

func newPasswordStore() *passwordStore {
	return &passwordStore{passwords: make(map[int]string)}
}

func (ps *passwordStore) Get(slotId int) (string, bool) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	p, ok := ps.passwords[slotId]
	return p, ok
}

func (ps *passwordStore) Set(slotId int, password string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.passwords[slotId] = password
}

func (ps *passwordStore) Delete(slotId int) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	delete(ps.passwords, slotId)
}

type apxServer struct {
	logf         func(f string, v ...any)
	config       *Config
	roomInfo     RoomInfoMessage
	roomPlayers  *RoomPlayers // Immutable
	passwords    *passwordStore
	bounceInfo   *bounceInfoStore
	connections  *connectionRegistry
	datapackages *DataPackageStore
	metrics      *metrics
	lobbyRoomId  string
}

// No strict lock, but this MUST be immutable to be safe
type registeredClient struct {
	slotId     int
	game       *string
	cancel     context.CancelFunc
	clientConn *websocket.Conn
}

// Stores data from all connected clients which is needed globally
type connectionRegistry struct {
	mu      sync.RWMutex
	clients map[int][]*registeredClient
	// Tags being covered here means registeredClient can stay immutable
	tags map[*registeredClient][]string
}

func newConnectionRegistry() *connectionRegistry {
	return &connectionRegistry{
		clients: make(map[int][]*registeredClient),
		tags:    make(map[*registeredClient][]string),
	}
}

func (cr *connectionRegistry) Register(slotId int, client *registeredClient, tags []string) {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	cr.clients[slotId] = append(cr.clients[slotId], client)
	cr.tags[client] = tags
}

// There HAS to be a safer way of doing this surely
func (cr *connectionRegistry) UpdateTags(client *registeredClient, tags []string) {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	cr.tags[client] = tags
}

func (cr *connectionRegistry) Kick(slotId int) {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	// Disconnect all clients to this slot
	clients := cr.clients[slotId]
	for _, client := range clients {
		client.cancel()
		delete(cr.tags, client)
	}
	delete(cr.clients, slotId)
}

func (cr *connectionRegistry) Unregister(client *registeredClient) {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	// Remove client from slot names arrays
	clients := cr.clients[client.slotId]
	i := slices.Index(clients, client)
	if i < 0 {
		return
	}
	cr.clients[client.slotId] = slices.Delete(clients, i, i+1)
	if len(cr.clients[client.slotId]) == 0 {
		delete(cr.clients, client.slotId)
	}

	delete(cr.tags, client)
	if len(cr.clients[client.slotId]) == 0 {
		delete(cr.clients, client.slotId)
	}
}

func (cr *connectionRegistry) BroadcastBounceFromSlot(ctx context.Context, bounceInfo *bounceInfoStore, slotId int, msg BounceMessage) {
	// Strip excluded tags
	msg.Tags = slices.DeleteFunc(msg.Tags, func(tag string) bool {
		return bounceInfo.IsExcluded(slotId, tag)
	})
	cr.BroadcastBounce(ctx, msg)
}

func (cr *connectionRegistry) BroadcastBounce(ctx context.Context, msg BounceMessage) {
	msgTagSet := buildTagSet(msg.Tags)

	// Lock because client tags are mutable
	cr.mu.RLock()
	var targets []*registeredClient
	for _, clients := range cr.clients {
		for _, c := range clients {
			if hasTagOverlap(cr.tags[c], msgTagSet) || hasSlotOverlap(c.slotId, msg.Slots) || hasGameOverlap(*c.game, msg.Games) {
				targets = append(targets, c)
			}
		}
	}
	cr.mu.RUnlock()

	// Content is the same, just a different cmd sending out
	msg.Cmd = "Bounced"
	for _, c := range targets {
		_ = wsjson.Write(ctx, c.clientConn, []any{msg})
	}
}

type connectionState struct {
	authenticated        bool
	slotName             *string
	cancel               context.CancelFunc
	clientConn           *websocket.Conn
	apConn               *websocket.Conn
	reduced              bool
	pendingDatapackGames []string
	registeredClient     *registeredClient
}

type MessageType string
type Permission int

const (
	wsReadLimit = 1 << 24 // 16 MB
)

type apxHandler struct {
	server  *apxServer
	reduced bool
}

// Put options on the handler so we can run 2 servers with the same apxServer backing them
func (h apxHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.server.serveConn(w, r, h.reduced)
}

func (s apxServer) serveConn(w http.ResponseWriter, r *http.Request, reduced bool) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Adds about 32mb memory usage per 1k connections, debatble CPU usage
		CompressionMode:    websocket.CompressionContextTakeover,
		InsecureSkipVerify: true,
	})
	if err != nil {
		s.logf("client connection: %v", err)
		return
	}
	defer c.CloseNow()

	c.SetReadLimit(wsReadLimit)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Send RoomInfo as opening message
	if err := wsjson.Write(ctx, c, []any{s.roomInfo}); err != nil {
		s.logf("failed to send RoomInfo: %v", err)
		return
	}

	var messages []map[string]any
	connState := &connectionState{
		authenticated:        false,
		cancel:               cancel,
		clientConn:           c,
		reduced:              reduced,
		pendingDatapackGames: []string{},
	}
	defer func() {
		if connState.registeredClient != nil {
			s.connections.Unregister(connState.registeredClient)
		}
		if connState.apConn != nil {
			connState.apConn.CloseNow()
		}
	}()

	for {
		err = wsjson.Read(ctx, c, &messages)
		if err != nil {
			if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
				s.logf("client read: %v", err)
			}
			return
		}

		for _, message := range messages {
			cmd, ok := message["cmd"].(string)
			if !ok {
				s.logf("message missing or invalid cmd field: %v", message)
				continue
			}

			if connState.authenticated {
				s.metrics.incomingPackets.WithLabelValues(s.lobbyRoomId, *connState.slotName, cmd).Inc()
			}

			if err := s.handleMessage(ctx, connState, MessageType(cmd), message); err != nil {
				if !isNormalClose(err) && ctx.Err() == nil {
					s.logf("client read: %v", err)
				}
			}
		}
	}
}

func (s apxServer) handleMessage(ctx context.Context, connState *connectionState, cmd MessageType, raw map[string]any) error {
	// Match against each message type we want to intercept from client -> server
	switch cmd {
	case MessageTypeGetDataPackage:
		return s.handleGetDataPackage(ctx, connState, raw)
	}

	if connState.authenticated {
		switch cmd {
		case MessageTypeBounce:
			return s.handleBounce(ctx, connState, raw)
		case MessageTypeConnectUpdate:
			return s.handleConnectUpdate(ctx, connState, raw)
		default:
			// We're authed, it's a message we don't care about, pass it on
			if connState.apConn != nil {
				return wsjson.Write(ctx, connState.apConn, []any{raw})
			}
			s.logf("unknown command: %q", cmd)
			return nil
		}
	} else {
		// Limit routes for unauthed clients so we don't need to check in every handler
		// Also lets us ignore duplicate connect messages
		switch cmd {
		case MessageTypeConnect:
			return s.handleConnect(ctx, connState, raw)
		default:
			s.logf("unknown command: %q", cmd)
			return nil
		}
	}
}

// Debatable whether this way of using tags as a set/map is even saving time, but eh, what's the harm
func hasTagOverlap(clientTags []string, msgTagSet map[string]struct{}) bool {
	for _, t := range clientTags {
		if _, ok := msgTagSet[t]; ok {
			return true
		}
	}
	return false
}

func hasSlotOverlap(slotID int, slots []int) bool {
	return slices.Contains(slots, slotID)
}

func hasGameOverlap(game string, games []string) bool {
	return slices.Contains(games, game)
}

// Turns an array into an O(1) map
func buildTagSet(tags []string) map[string]struct{} {
	set := make(map[string]struct{}, len(tags))
	for _, t := range tags {
		set[t] = struct{}{}
	}
	return set
}
