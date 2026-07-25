package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
)

type apiServer struct {
	config        *Config
	passwords     *passwordStore
	bounceInfo    *bounceInfoStore
	wsConnections *connectionRegistry
}

type Deathlink struct {
	Slot      string  `json:"slot"`
	Source    string  `json:"source"`
	Cause     *string `json:"cause"`
	CreatedAt string  `json:"created_at"`
}

func startApiServer(cfg *Config, passwords *passwordStore, bounceInfo *bounceInfoStore, wsConnections *connectionRegistry) *http.Server {
	srv := &apiServer{
		config:        cfg,
		passwords:     passwords,
		bounceInfo:    bounceInfo,
		wsConnections: wsConnections,
	}

	r := mux.NewRouter()
	api := r.PathPrefix("/api").Subrouter()
	api.Use(srv.loggingMiddleware)
	api.Use(srv.authMiddleware)
	api.HandleFunc("/refresh_passwords", srv.handlePasswordRefresh).Methods(http.MethodPost)
	api.HandleFunc("/deathlinks", srv.handleDeathlinks).Methods(http.MethodGet)
	api.HandleFunc("/bounce_exclusions", srv.handleBounceExclusionsList).Methods(http.MethodGet)
	api.HandleFunc("/bounce_exclusions/{slotname}/{tag}", srv.handleBounceExclusions).Methods(http.MethodPost, http.MethodDelete)
	api.HandleFunc("/deathlink_probability", srv.handleProbability).Methods(http.MethodGet, http.MethodPost)

	s := &http.Server{
		Addr:         cfg.ApiListenAddr,
		Handler:      r,
		ReadTimeout:  time.Second * 10,
		WriteTimeout: time.Second * 10,
	}

	log.Printf("API server listening on http://%s", cfg.ApiListenAddr)
	go func() {
		if err := s.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("API server error: %v", err)
		}
	}()

	return s
}

func (a *apiServer) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("X-API-Key")
		if a.config.ApiKey == "" || key != a.config.ApiKey {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *apiServer) handlePasswordRefresh(w http.ResponseWriter, r *http.Request) {
	if !a.config.LobbyEnabled {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "lobby not enabled"})
		return
	}

	slots, err := fetchSlotPasswords(a.config)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to fetch passwords from lobby"})
		return
	}
	loadPasswordsIntoStore(a.wsConnections, a.passwords, slots)
}

func (a *apiServer) handleDeathlinks(w http.ResponseWriter, r *http.Request) {
	deathlinks := a.bounceInfo.Get()
	json.NewEncoder(w).Encode(deathlinks)
}

func (a *apiServer) handleBounceExclusionsList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(a.bounceInfo.GetExclusions())
}

func (a *apiServer) handleBounceExclusions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	vars := mux.Vars(r)
	slotName := vars["slotname"]
	tag := vars["tag"]

	switch r.Method {
	case http.MethodPost:
		a.bounceInfo.Exclude(slotName, tag)
		json.NewEncoder(w).Encode(map[string]any{"excluded": true})

	case http.MethodDelete:
		a.bounceInfo.Unexclude(slotName, tag)
		json.NewEncoder(w).Encode(map[string]any{"excluded": false})
	}
}

func (a *apiServer) handleProbability(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		json.NewEncoder(w).Encode(map[string]any{"probability": a.bounceInfo.GetProbability()})

	case http.MethodPost:
		var body struct {
			Probability *float64 `json:"probability"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
			return
		}
		if body.Probability != nil && (*body.Probability < 0 || *body.Probability > 1) {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "probability must be between 0 and 1"})
			return
		}
		a.bounceInfo.SetProbability(*body.Probability)
		json.NewEncoder(w).Encode(map[string]any{"probability": a.bounceInfo.GetProbability()})
	}
}

func (a *apiServer) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		route := mux.CurrentRoute(r)
		path, _ := route.GetPathTemplate()
		log.Printf("%s %s (matched: %s) %s", r.Method, r.URL.Path, path, time.Since(start))
	})
}
