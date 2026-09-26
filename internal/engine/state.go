package engine

import (
	"context"
	"sync"
	"time"
)

func init() {
	RegisterStateBackend("memory", nil, func(e *Engine) (StateStore, error) {
		return newMemoryState(e.StateTTL()), nil
	})
}

type memEntry struct {
	v   map[string]any
	exp time.Time
}

// memoryState is the default state store; lost on restart.
type memoryState struct {
	mu  sync.RWMutex
	m   map[string]memEntry
	ttl time.Duration
}

func newMemoryState(ttl time.Duration) *memoryState {
	s := &memoryState{m: map[string]memEntry{}, ttl: ttl}
	if ttl > 0 {
		go func() {
			for range time.Tick(ttl) {
				now := time.Now()
				s.mu.Lock()
				for k, v := range s.m {
					if now.After(v.exp) {
						delete(s.m, k)
					}
				}
				s.mu.Unlock()
			}
		}()
	}
	return s
}

func (s *memoryState) Get(_ context.Context, key string) (map[string]any, error) {
	s.mu.RLock()
	en, ok := s.m[key]
	s.mu.RUnlock()
	if !ok || (s.ttl > 0 && time.Now().After(en.exp)) {
		return nil, nil
	}
	// Copy so a flow cannot mutate the stored map without saving.
	out := make(map[string]any, len(en.v))
	for k, v := range en.v {
		out[k] = v
	}
	return out, nil
}

func (s *memoryState) Set(_ context.Context, key string, v map[string]any) error {
	s.mu.Lock()
	s.m[key] = memEntry{v: v, exp: time.Now().Add(s.ttl)}
	s.mu.Unlock()
	return nil
}

func (s *memoryState) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	delete(s.m, key)
	s.mu.Unlock()
	return nil
}
