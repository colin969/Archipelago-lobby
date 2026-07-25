package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand/v2"
	"slices"
	"sync"
)

type bounceInfoStore struct {
	mu sync.RWMutex
	// Maps slot name to deathlink count
	counts map[string]int
	// Probability to accept a deathlink message
	deathlinkProbability float64
	// Slots that aren't allowed to send certain tags
	// Absurd typing, but should be O(1) because it's all keys in a map, fite me
	excluded map[string]map[string]struct{}
}

func newBounceInfoStore() *bounceInfoStore {
	return &bounceInfoStore{
		counts:               make(map[string]int),
		deathlinkProbability: 1,
		excluded:             make(map[string]map[string]struct{}),
	}
}

func (ds *bounceInfoStore) Get() map[string]int {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	result := make(map[string]int, len(ds.counts))
	for k, v := range ds.counts {
		result[k] = v
	}
	return result
}

func (ds *bounceInfoStore) GetProbability() float64 {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	return ds.deathlinkProbability
}

func (ds *bounceInfoStore) SetProbability(probability float64) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.deathlinkProbability = probability
}

func (ds *bounceInfoStore) GetExclusions() map[string][]string {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	// Make a safe copy of it to return
	result := make(map[string][]string, len(ds.excluded))
	for slot, tags := range ds.excluded {
		tagList := make([]string, 0, len(tags))
		for tag := range tags {
			tagList = append(tagList, tag)
		}
		result[slot] = tagList
	}
	return result
}

func (ds *bounceInfoStore) Exclude(slotName, tag string) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	if ds.excluded[slotName] == nil {
		ds.excluded[slotName] = make(map[string]struct{})
	}
	ds.excluded[slotName][tag] = struct{}{}
}

func (ds *bounceInfoStore) Unexclude(slotName, tag string) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	delete(ds.excluded[slotName], tag)
	if len(ds.excluded[slotName]) == 0 {
		delete(ds.excluded, slotName)
	}
}

func (ds *bounceInfoStore) IsExcluded(slotName, tag string) bool {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	_, ok := ds.excluded[slotName][tag]
	return ok
}

// Add to the slot's deathlink count
func (ds *bounceInfoStore) Add(slotName string) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.counts[slotName]++
}

func (s apxServer) handleBounce(ctx context.Context, connState *connectionState, raw map[string]any) error {
	data, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("marshalling bounce message: %w", err)
	}

	var msg BounceMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return fmt.Errorf("unmarshalling bounce message: %w", err)
	}

	// Deathlink packets have extra options for blocking and probability limiting, handle seperate
	if slices.Contains(msg.Tags, "DeathLink") {
		return s.handleDeathLink(ctx, connState, msg)
	}

	// Strip any excluded tags
	msg.Tags = slices.DeleteFunc(msg.Tags, func(tag string) bool {
		return s.bounceInfo.IsExcluded(*connState.slotName, tag)
	})

	s.connections.BroadcastBounceFromSlot(ctx, s.bounceInfo, *connState.slotName, msg)

	return nil
}

func (s apxServer) handleDeathLink(ctx context.Context, connState *connectionState, msg BounceMessage) error {
	// Validate this is a valid packet. Some apworlds send bad packets, some can't handle being sent bad packets.
	data, err := json.Marshal(msg.Data)
	if err != nil {
		return fmt.Errorf("marshalling deathlink data: %w", err)
	}

	var dl BounceDataDeathlink
	if err := json.Unmarshal(data, &dl); err != nil {
		return fmt.Errorf("unmarshalling deathlink data: %w", err)
	}

	s.bounceInfo.Add(*connState.slotName)
	log.Printf("deathlink: slot=%q source=%q cause=%v", *connState.slotName, dl.Source, dl.Cause)

	// Strip any excluded tags
	msg.Tags = slices.DeleteFunc(msg.Tags, func(tag string) bool {
		return s.bounceInfo.IsExcluded(*connState.slotName, tag)
	})

	if s.bounceInfo.IsExcluded(*connState.slotName, "DeathLink") {
		log.Printf("deathlink blocked for excluded slot %q", *connState.slotName)
		return nil
	}

	probability := s.bounceInfo.GetProbability()
	if probability != 1 && rand.Float64() >= probability {
		log.Println("deathlink dropped by probability func")
		return nil
	}

	s.connections.BroadcastBounceFromSlot(ctx, s.bounceInfo, *connState.slotName, msg)

	return nil
}
