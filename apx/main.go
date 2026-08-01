package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type Config struct {
	ListenAddr        string `json:"apx_ws_listenaddr"`
	ReducedListenAddr string `json:"apx_ws_reduced_listenaddr"`
	APHost            string `json:"ap_room_host"`
	APPort            int    `json:"ap_room_port"`
	APPassword        string `json:"ap_room_password"`
	LobbyEnabled      bool   `json:"lobby_enabled"`
	LobbyRootUrl      string `json:"lobby_root_url"`
	LobbyRoomId       string `json:"lobby_room_id"`
	LobbyApiKey       string `json:"lobby_api_key"`
	ApiListenAddr     string `json:"apx_api_listen"`
	ApiKey            string `json:"apx_api_key"`
	ApRoomId          string `json:"ap_room_id"`
	ApApiRoot         string `json:"ap_api_root"`
}

func main() {
	err := run()
	if err != nil {
		log.Fatal(err)
	}
}

// run starts a http.Server for the passed in address
// with all requests handled by echoServer.
func run() error {
	cfg, err := getConfig()
	if err != nil {
		return err
	}

	roomPlayers, err := fetchRoomPlayers(cfg.ApApiRoot, cfg.ApRoomId)
	if err != nil {
		return fmt.Errorf("failed to get %s/api/room_players/%s from AP server, aborting: %w", cfg.ApApiRoot, cfg.ApRoomId, err)
	}

	roomInfo, err := connectAndGetRoomInfo(cfg.APHost, cfg.APPort)
	if err != nil {
		return fmt.Errorf("failed to get RoomInfo from AP server, aborting: %w", err)
	}

	if cfg.LobbyEnabled {
		roomInfo.Password = true
	}

	wsListener, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return err
	}
	log.Printf("listening on ws://%v", wsListener.Addr())

	wsReducedListener, err := net.Listen("tcp", cfg.ReducedListenAddr)
	if err != nil {
		return err
	}
	log.Printf("reduced listening on ws://%v", wsReducedListener.Addr())

	passwordStore := newPasswordStore()
	connRegistry := newConnectionRegistry()
	datapackageCache := newDatapackageCache()
	bounceInfo := newBounceInfoStore()
	if cfg.LobbyEnabled {
		slots, err := fetchSlotPasswords(cfg)
		if err != nil {
			return fmt.Errorf("failed to fetch slot passwords: %w", err)
		}
		loadPasswordsIntoStore(connRegistry, passwordStore, roomPlayers, slots)
	}

	startApiServer(cfg, passwordStore, bounceInfo, connRegistry, roomPlayers)

	srv := apxServer{
		logf:         log.Printf,
		config:       cfg,
		roomInfo:     *roomInfo,
		roomPlayers:  roomPlayers,
		passwords:    passwordStore,
		connections:  connRegistry,
		bounceInfo:   bounceInfo,
		datapackages: datapackageCache,
	}

	// Normal traffic
	normalServer := &http.Server{
		Handler:      apxHandler{server: srv, reduced: false},
		ReadTimeout:  time.Second * 10,
		WriteTimeout: time.Second * 10,
	}

	// Reduced traffic (e.g less PrintJSON messages)
	reducedServer := &http.Server{
		Handler:      apxHandler{server: srv, reduced: true},
		ReadTimeout:  time.Second * 10,
		WriteTimeout: time.Second * 10,
	}

	errc := make(chan error, 1)
	go func() {
		errc <- normalServer.Serve(wsListener)
	}()
	go func() {
		errc <- reducedServer.Serve(wsReducedListener)
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt)
	select {
	case err := <-errc:
		log.Printf("failed to serve: %v", err)
	case sig := <-sigs:
		log.Printf("terminating: %v", sig)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	return normalServer.Shutdown(ctx)
}

func loadConfigFile(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cfg Config
	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}
	return &cfg, nil
}

func getConfig() (*Config, error) {
	cfg := &Config{}

	// Try config file first
	configPath := os.Getenv("CONFIG_FILE")
	if configPath == "" {
		configPath = "config.json"
	}
	if fileCfg, err := loadConfigFile(configPath); err == nil {
		cfg = fileCfg
		log.Printf("loaded config from %s", configPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("error reading config file: %w", err)
	}

	// Env vars override config file
	if v := os.Getenv("APX_WS_LISTENADDR"); v != "" {
		cfg.ListenAddr = v
	}
	if v := os.Getenv("APX_WS_REDUCED_LISTENADDR"); v != "" {
		cfg.ReducedListenAddr = v
	}
	if v := os.Getenv("AP_ROOM_HOST"); v != "" {
		cfg.APHost = v
	}
	if v := os.Getenv("AP_ROOM_PORT"); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid AP_ROOM_PORT %q: %w", v, err)
		}
		cfg.APPort = port
	}
	if v := os.Getenv("AP_ROOM_PASSWORD"); v != "" {
		cfg.APPassword = v
	}
	if v := os.Getenv("LOBBY_ENABLED"); v != "" {
		enabled, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("invalid LOBBY_ENABLED %q: %w", v, err)
		}
		cfg.LobbyEnabled = enabled
	}
	if v := os.Getenv("LOBBY_ROOT_URL"); v != "" {
		cfg.LobbyRootUrl = v
	}
	if v := os.Getenv("LOBBY_ROOM_ID"); v != "" {
		cfg.LobbyRoomId = v
	}
	if v := os.Getenv("LOBBY_API_KEY"); v != "" {
		cfg.LobbyApiKey = v
	}
	if v := os.Getenv("APX_API_LISTENADDR"); v != "" {
		cfg.ApiListenAddr = v
	}
	if v := os.Getenv("APX_API_KEY"); v != "" {
		cfg.ApiKey = v
	}
	if v := os.Getenv("AP_ROOM_ID"); v != "" {
		cfg.ApRoomId = v
	}
	if v := os.Getenv("AP_API_ROOT"); v != "" {
		cfg.ApApiRoot = v
	}

	if cfg.APPort < 1 || cfg.APPort > 65535 {
		return nil, fmt.Errorf("port %d out of range (1-65535)", cfg.APPort)
	}

	return cfg, nil
}

// Get the room info from the AP multiserver so we can cache it
func connectAndGetRoomInfo(apHost string, apPort int) (*RoomInfoMessage, error) {
	url := fmt.Sprintf("ws://%s:%d", apHost, apPort)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Archipelago server at %s: %w", url, err)
	}
	defer c.CloseNow()

	var raw []map[string]any
	if err := wsjson.Read(ctx, c, &raw); err != nil {
		return nil, fmt.Errorf("failed to read initial message: %w", err)
	}

	for _, msg := range raw {
		cmd, _ := msg["cmd"].(string)
		if cmd != "RoomInfo" {
			continue
		}

		data, err := json.Marshal(msg)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal RoomInfo: %w", err)
		}

		var roomInfo RoomInfoMessage
		if err := json.Unmarshal(data, &roomInfo); err != nil {
			return nil, fmt.Errorf("failed to parse RoomInfo: %w", err)
		}

		log.Printf("connected to AP server: seed=%q", roomInfo.SeedName)
		return &roomInfo, nil
	}

	return nil, errors.New("no RoomInfo packet received in initial message")
}

type RoomPlayers map[string][2]int

func fetchRoomPlayers(apApiRoot string, apRoomId string) (RoomPlayers, error) {
	url := fmt.Sprintf("%s/api/room_players/%s", apApiRoot, apRoomId)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetching room players: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from /api/room_players", resp.StatusCode)
	}

	var raw map[string][2]int
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decoding room players: %w", err)
	}

	return RoomPlayers(raw), nil
}
