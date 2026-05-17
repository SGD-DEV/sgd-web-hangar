package services

import (
	"fmt"
	"sync"
)

type Manager struct {
	services map[string]Service
	mu       sync.RWMutex
}

func NewManager() *Manager {
	return &Manager{
		services: make(map[string]Service),
	}
}

func (m *Manager) Register(name string, svc Service) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.services[name] = svc
}

func (m *Manager) Get(name string) (Service, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	svc, ok := m.services[name]
	if !ok {
		return nil, fmt.Errorf("services: %q not registered", name)
	}
	return svc, nil
}

func (m *Manager) Start(name string) error {
	svc, err := m.Get(name)
	if err != nil {
		return err
	}
	if err := m.checkWebServerConflict(name); err != nil {
		return err
	}
	return svc.Start()
}

// checkWebServerConflict prevents Apache and Nginx from being started at the
// same time — they both bind 80/443 by default, and the raw nginx/Apache error
// in that case ("bind() to 0.0.0.0:443 failed (10013: ... access permissions)")
// is opaque. Surfacing a clear message before launch is much friendlier.
func (m *Manager) checkWebServerConflict(starting string) error {
	var sibling string
	switch starting {
	case "nginx":
		sibling = "apache"
	case "apache":
		sibling = "nginx"
	default:
		return nil
	}

	other, err := m.Get(sibling)
	if err != nil {
		return nil // sibling not registered — nothing to conflict with
	}
	st := other.Status()
	if st.Status != StatusRunning {
		return nil
	}

	// Capitalize for the message — "Apache" / "Nginx" reads better than the
	// lowercase service identifiers used internally.
	pretty := func(s string) string {
		switch s {
		case "nginx":
			return "Nginx"
		case "apache":
			return "Apache"
		}
		return s
	}
	return fmt.Errorf(
		"cannot start %s: %s is already running and uses the same ports (80/443). "+
			"Stop %s first, then start %s.",
		pretty(starting), pretty(sibling), pretty(sibling), pretty(starting),
	)
}

func (m *Manager) Stop(name string) error {
	svc, err := m.Get(name)
	if err != nil {
		return err
	}
	return svc.Stop()
}

func (m *Manager) Restart(name string) error {
	svc, err := m.Get(name)
	if err != nil {
		return err
	}
	return svc.Restart()
}

func (m *Manager) Status(name string) (ServiceStatus, error) {
	svc, err := m.Get(name)
	if err != nil {
		return ServiceStatus{}, err
	}
	return svc.Status(), nil
}

func (m *Manager) AllStatuses() map[string]ServiceStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]ServiceStatus, len(m.services))
	for name, svc := range m.services {
		result[name] = svc.Status()
	}
	return result
}

func (m *Manager) StartAll() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var firstErr error
	for name, svc := range m.services {
		if svc.IsInstalled() {
			if err := m.checkWebServerConflict(name); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			if err := svc.Start(); err != nil && firstErr == nil {
				firstErr = fmt.Errorf("services: starting %s: %w", name, err)
			}
		}
	}
	return firstErr
}

func (m *Manager) StopAll() {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, svc := range m.services {
		svc.Stop()
	}
}

func (m *Manager) Names() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.services))
	for name := range m.services {
		names = append(names, name)
	}
	return names
}
