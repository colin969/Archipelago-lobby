package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Spheres []SphereLocations

// { slot_id : [loc_id, loc_id, loc_id] }
type SphereLocations map[int32][]int64

type SphereResult struct {
	Locations []LocationEntry `json:"locations"`
	Total     int             `json:"total"`
	Checked   int             `json:"checked"`
}

type apiServer struct {
	config  *Config
	apx     *apxServer
	spheres Spheres
	// e.g locationIdToName["Ocarina of Time"][3] == "Song from Saria"
	locationIdToName     map[string]map[int]string
	checkedLocations     map[int]map[int64]bool
	sphereCache          *slotSphereCache
	completeSphere1Slots map[int]struct{}
}

type Deathlink struct {
	Slot      string  `json:"slot"`
	Source    string  `json:"source"`
	Cause     *string `json:"cause"`
	CreatedAt string  `json:"created_at"`
}

type LocationEntry struct {
	ID      int64
	Name    string
	Checked bool
}

func (l LocationEntry) MarshalJSON() ([]byte, error) {
	return json.Marshal([3]any{l.ID, l.Name, l.Checked})
}

// Cache sphere responses to save compute
type slotSphereCache struct {
	mu    sync.RWMutex
	data  map[int][]byte // slotId -> pre-encoded JSON
	dirty map[int]bool   // slotId -> needs rebuild
}

func newSlotSphereCache() *slotSphereCache {
	return &slotSphereCache{
		data:  make(map[int][]byte),
		dirty: make(map[int]bool),
	}
}

func (c *slotSphereCache) Invalidate(slotId int) {
	c.mu.Lock()
	c.dirty[slotId] = true
	c.mu.Unlock()
}

func (c *slotSphereCache) Get(slotId int) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.dirty[slotId] {
		return nil, false
	}
	b, ok := c.data[slotId]
	return b, ok
}

func (c *slotSphereCache) Set(slotId int, b []byte) {
	c.mu.Lock()
	c.data[slotId] = b
	c.dirty[slotId] = false
	c.mu.Unlock()
}

func startApiServer(cfg *Config, apx *apxServer) *http.Server {
	// Prefetch sphere locations
	spheres, err := fetchRoomSpheres(cfg.ApApiRoot, cfg.ApRoomId)
	if err != nil {
		log.Fatalf("prefetching spheres: %v", err)
	}

	srv := &apiServer{
		config:               cfg,
		apx:                  apx,
		spheres:              spheres,
		sphereCache:          newSlotSphereCache(),
		completeSphere1Slots: map[int]struct{}{},
	}

	if err := srv.refreshCheckedLocations(cfg.ApApiRoot, cfg.ApRoomId); err != nil {
		log.Printf("initial checked locations fetch failed: %v", err)
	}
	srv.startCheckedLocationPoller(cfg.ApApiRoot, cfg.ApRoomId, 30*time.Second)

	r := mux.NewRouter()
	api := r.PathPrefix("/api").Subrouter()
	api.Use(srv.loggingMiddleware)
	api.Use(srv.authMiddleware)
	api.HandleFunc("/refresh_passwords", srv.handlePasswordRefresh).Methods(http.MethodPost)
	api.HandleFunc("/password/{slotId}", srv.handlePassword).Methods(http.MethodGet)
	api.HandleFunc("/deathlinks", srv.handleDeathlinks).Methods(http.MethodGet)
	api.HandleFunc("/bounce_exclusions", srv.handleBounceExclusionsList).Methods(http.MethodGet)
	api.HandleFunc("/bounce_exclusions/{slotId}/{tag}", srv.handleBounceExclusions).Methods(http.MethodPost, http.MethodDelete)
	api.HandleFunc("/deathlink_probability", srv.handleProbability).Methods(http.MethodGet, http.MethodPost)
	api.HandleFunc("/spheres", srv.handleAllSpheres).Methods(http.MethodGet)
	api.HandleFunc("/incomplete_sphere1", srv.handleIncompleteSphere1).Methods(http.MethodGet)
	api.HandleFunc("/spheres/{slotId}", srv.handleSpheresForSlot).Methods(http.MethodGet)
	api.HandleFunc("/metrics", promhttp.HandlerFor(apx.reg, promhttp.HandlerOpts{}).ServeHTTP)

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
	loadPasswordsIntoStore(a.apx.connections, a.apx.passwords, a.apx.roomPlayers, slots)
}

func (a *apiServer) handlePassword(w http.ResponseWriter, r *http.Request) {
	if !a.config.LobbyEnabled {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "lobby not enabled"})
		return
	}

	vars := mux.Vars(r)
	slotId, err := strconv.Atoi(vars["slotId"])
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid slotId"})
		return
	}

	password, ok := a.apx.passwords.Get(slotId)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "no password for slot"})
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"password": password})
}

func (a *apiServer) handleDeathlinks(w http.ResponseWriter, r *http.Request) {
	deathlinks := a.apx.bounceInfo.Get()
	json.NewEncoder(w).Encode(deathlinks)
}

func (a *apiServer) handleBounceExclusionsList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(a.apx.bounceInfo.GetExclusions())
}

func (a *apiServer) handleBounceExclusions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	vars := mux.Vars(r)
	slotId, err := strconv.Atoi(vars["slotId"])
	if err != nil {
		http.Error(w, "invalid slotId", http.StatusBadRequest)
		return
	}
	tag := vars["tag"]
	if tag == "" {
		http.Error(w, "missing tag", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodPost:
		a.apx.bounceInfo.Exclude(slotId, tag)
		json.NewEncoder(w).Encode(map[string]any{"excluded": true})

	case http.MethodDelete:
		a.apx.bounceInfo.Unexclude(slotId, tag)
		json.NewEncoder(w).Encode(map[string]any{"excluded": false})
	}
}

func (a *apiServer) handleProbability(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		json.NewEncoder(w).Encode(map[string]any{"probability": a.apx.bounceInfo.GetProbability()})

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
		a.apx.bounceInfo.SetProbability(*body.Probability)
		json.NewEncoder(w).Encode(map[string]any{"probability": a.apx.bounceInfo.GetProbability()})
	}
}

func (a *apiServer) handleSpheresForSlot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	vars := mux.Vars(r)
	slotId, err := strconv.Atoi(vars["slotId"])
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid slotId"})
		return
	}

	// Check for cache hit first
	if cached, ok := a.sphereCache.Get(slotId); ok {
		w.Write(cached)
		return
	}
	slot, ok := a.apx.roomPlayers.slots[slotId]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "slot not found"})
		return
	}

	locationIDToName := a.apx.datapackages.LocationIDToName[slot.Game]
	checkedLocations := a.checkedLocations[slotId]

	result := make([]SphereResult, 0, len(a.spheres))
	for _, sphere := range a.spheres {
		locIDs, ok := sphere[int32(slotId)]
		if !ok {
			continue
		}
		entries := make([]LocationEntry, 0, len(locIDs))
		locs_checked := 0
		for _, locID := range locIDs {
			checked := checkedLocations[locID]
			if checked {
				locs_checked++
			}
			entries = append(entries, LocationEntry{
				ID:      locID,
				Name:    locationIDToName[locID],
				Checked: checked,
			})
		}
		result = append(result, SphereResult{
			Locations: entries,
			Total:     len(entries),
			Checked:   locs_checked,
		})
	}

	b, err := json.Marshal(result)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	a.sphereCache.Set(slotId, b)
	w.Write(b)
}

func (a *apiServer) handleAllSpheres(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	result := make(map[string]json.RawMessage, len(a.apx.roomPlayers.slots))

	for slotId, slot := range a.apx.roomPlayers.slots {
		if cached, ok := a.sphereCache.Get(slotId); ok {
			result[slot.Name] = json.RawMessage(cached)
			continue
		}

		locationIDToName := a.apx.datapackages.LocationIDToName[slot.Game]
		checkedLocations := a.checkedLocations[slotId]

		slotResult := make([]SphereResult, 0, len(a.spheres))
		for _, sphere := range a.spheres {
			locIDs, ok := sphere[int32(slotId)]
			if !ok {
				continue
			}
			entries := make([]LocationEntry, 0, len(locIDs))
			locsChecked := 0
			for _, locID := range locIDs {
				checked := checkedLocations[locID]
				if checked {
					locsChecked++
				}
				entries = append(entries, LocationEntry{
					ID:      locID,
					Name:    locationIDToName[locID],
					Checked: checked,
				})
			}
			slotResult = append(slotResult, SphereResult{
				Locations: entries,
				Total:     len(entries),
				Checked:   locsChecked,
			})
		}

		b, err := json.Marshal(slotResult)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		a.sphereCache.Set(slotId, b)
		result[slot.Name] = json.RawMessage(b)
	}

	if err := json.NewEncoder(w).Encode(result); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func (a *apiServer) handleIncompleteSphere1(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if len(a.spheres) == 0 {
		json.NewEncoder(w).Encode([]int{})
		return
	}

	sphere1 := a.spheres[0]

	// Check if status of incomplete slots has changed
	for slotId, locIDs := range sphere1 {
		if _, done := a.completeSphere1Slots[int(slotId)]; done {
			continue
		}
		if !isSphere1Incomplete(locIDs, a.checkedLocations[int(slotId)]) {
			a.completeSphere1Slots[int(slotId)] = struct{}{}
		}
	}

	// Build list of all incomplete slots
	result := make([]int, 0)
	for slotId := range a.apx.roomPlayers.slots {
		if _, done := a.completeSphere1Slots[slotId]; !done {
			result = append(result, slotId)
		}
	}
	json.NewEncoder(w).Encode(result)
}

func isSphere1Incomplete(locIDs []int64, checkedLocations map[int64]bool) bool {
	for _, locID := range locIDs {
		if !checkedLocations[locID] {
			return true
		}
	}
	return false
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

func (a *apiServer) startCheckedLocationPoller(apApiRoot, apRoomId string, interval time.Duration) {
	go func() {
		for {
			if err := a.refreshCheckedLocations(apApiRoot, apRoomId); err != nil {
				log.Printf("refreshing checked locations: %v", err)
			}
			time.Sleep(interval)
		}
	}()
}

func (a *apiServer) refreshCheckedLocations(apApiRoot, apRoomId string) error {
	url := fmt.Sprintf("%s/api/room/%s/checked_locations", apApiRoot, apRoomId)

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("fetching checked locations: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var raw map[string][]int64
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return fmt.Errorf("decoding checked locations: %w", err)
	}

	newChecked := make(map[int]map[int64]bool, len(raw))
	for slotStr, locIDs := range raw {
		slotId, err := strconv.Atoi(slotStr)
		if err != nil {
			return fmt.Errorf("invalid slot key %q: %w", slotStr, err)
		}
		locs := make(map[int64]bool, len(locIDs))
		for _, id := range locIDs {
			locs[id] = true
		}
		newChecked[slotId] = locs
	}

	// Invalidate cache for any slot whose checked locations changed
	for slotId, newLocs := range newChecked {
		oldLocs := a.checkedLocations[slotId]
		if len(oldLocs) != len(newLocs) {
			a.sphereCache.Invalidate(slotId)
		}
	}

	a.checkedLocations = newChecked
	return nil
}

func fetchRoomSpheres(apApiRoot string, apRoomId string) (Spheres, error) {
	url := fmt.Sprintf("%s/api/room/%s/spheres", apApiRoot, apRoomId)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetching room spheres: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from /api/room/%s/spheres", resp.StatusCode, apRoomId)
	}

	var spheres Spheres
	if err := json.NewDecoder(resp.Body).Decode(&spheres); err != nil {
		return nil, fmt.Errorf("decoding room spheres: %w", err)
	}

	return spheres, nil
}

// Slot IDs are keys, so JSON turns them to strings, we want the numbers
func (s *SphereLocations) UnmarshalJSON(data []byte) error {
	var raw map[string][]int64
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*s = make(SphereLocations, len(raw))
	for k, v := range raw {
		id, err := strconv.ParseInt(k, 10, 32)
		if err != nil {
			return fmt.Errorf("invalid slot_id key %q: %w", k, err)
		}
		(*s)[int32(id)] = v
	}
	return nil
}
