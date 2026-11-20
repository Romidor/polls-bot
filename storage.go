package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
)

type Store struct {
	mu       sync.Mutex
	basePath string
	Schedule []ScheduleSlot
	State    *State
}

func NewStore(basePath string) (*Store, error) {
	s := &Store{basePath: basePath, State: NewState()}

	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, err
	}

	if err := s.loadSchedule(); err != nil {
		return nil, err
	}
	if err := s.loadState(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) loadSchedule() error {
	path := filepath.Join(s.basePath, "schedule.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			s.Schedule = []ScheduleSlot{}
			return nil
		}
		return err
	}
	return json.Unmarshal(data, &s.Schedule)
}

func (s *Store) loadState() error {
	path := filepath.Join(s.basePath, "state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // State уже инициализирован в NewState
		}
		return err
	}
	if err := json.Unmarshal(data, s.State); err != nil {
		return err
	}
	// Гарантируем, что карты не nil после загрузки
	if s.State.Overrides == nil {
		s.State.Overrides = make(map[string]*Override)
	}
	if s.State.ActivePolls == nil {
		s.State.ActivePolls = make(map[string]*ActivePoll)
	}
	if s.State.PendingPolls == nil {
		s.State.PendingPolls = make(map[string]*PendingPoll)
	}
	return nil
}

func (s *Store) SaveSchedule() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return atomicWrite(filepath.Join(s.basePath, "schedule.json"), s.Schedule)
}

func (s *Store) SaveState() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return atomicWrite(filepath.Join(s.basePath, "state.json"), s.State)
}

func atomicWrite(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Lock/Unlock для составных операций с State
func (s *Store) Lock()   { s.mu.Lock() }
func (s *Store) Unlock() { s.mu.Unlock() }

func (s *Store) LogScheduleChange(action, detail, userID string) {
	log.Printf("[schedule] %s: %s by user %s", action, detail, userID)
}
