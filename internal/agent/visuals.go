package agent

// SetVisuals sets what the user's screen draws of an answer, told after the
// system prompt; empty for none. It is safe to call while a turn runs.
func (a *Agent) SetVisuals(note string) {
	a.modelMu.Lock()
	defer a.modelMu.Unlock()
	a.visuals = note
}

func (a *Agent) visualsNote() string {
	a.modelMu.RLock()
	defer a.modelMu.RUnlock()
	return a.visuals
}

// system is the system prompt as it is sent: the prompt, then what the
// screen draws.
func (a *Agent) system() string {
	a.modelMu.RLock()
	defer a.modelMu.RUnlock()
	if a.visuals == "" {
		return a.System
	}
	return a.System + "\n\n" + a.visuals
}
