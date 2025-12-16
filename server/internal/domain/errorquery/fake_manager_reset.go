package errorquery

// ClearGeneratedErrors clears all generated errors to allow fresh generation.
// This is useful for testing to ensure errors are regenerated with new probabilities.
func (m *FakeErrorManager) ClearGeneratedErrors() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Clear error index for generated errors
	for _, errors := range m.generatedErrors {
		for _, err := range errors {
			delete(m.errorIndex, err.ErrorID)
		}
	}

	// Clear generated errors map
	m.generatedErrors = make(map[string][]ErrorRecord)
}
