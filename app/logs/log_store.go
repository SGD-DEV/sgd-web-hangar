package logs

import (
	"sync"
	"time"
)

type Entry struct {
	Service   string `json:"service"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
}

type Store struct {
	entries    map[string][]Entry
	maxPerSvc  int
	mu         sync.RWMutex
}

func NewStore(maxPerService int) *Store {
	return &Store{
		entries:   make(map[string][]Entry),
		maxPerSvc: maxPerService,
	}
}

func (s *Store) Add(service, message string) {
	s.AddWithLevel(service, message, "info")
}

func (s *Store) AddWithLevel(service, message, level string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry := Entry{
		Service:   service,
		Message:   message,
		Timestamp: time.Now().Format(time.RFC3339),
		Level:     level,
	}

	s.entries[service] = append(s.entries[service], entry)

	if len(s.entries[service]) > s.maxPerSvc {
		excess := len(s.entries[service]) - s.maxPerSvc
		s.entries[service] = s.entries[service][excess:]
	}
}

func (s *Store) Get(service string, lines int) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries := s.entries[service]
	if len(entries) == 0 {
		return nil
	}

	start := 0
	if lines > 0 && lines < len(entries) {
		start = len(entries) - lines
	}

	result := make([]string, 0, len(entries)-start)
	for _, e := range entries[start:] {
		result = append(result, e.Timestamp+" ["+e.Level+"] "+e.Message)
	}
	return result
}

func (s *Store) GetEntries(service string, lines int) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries := s.entries[service]
	if len(entries) == 0 {
		return nil
	}

	start := 0
	if lines > 0 && lines < len(entries) {
		start = len(entries) - lines
	}

	result := make([]Entry, len(entries)-start)
	copy(result, entries[start:])
	return result
}

func (s *Store) Clear(service string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, service)
}

func (s *Store) ClearAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = make(map[string][]Entry)
}
