package ssl

// GetCARoot returns the folder holding Hangar's root CA (see CARoot).
func (m *Manager) GetCARoot() (string, error) {
	return m.CARoot(), nil
}
