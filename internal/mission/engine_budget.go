package mission

// reloadIfHeld reads the settings again when the mission is on hold, so a
// raised limit in missions.yaml is seen. A failure keeps the old settings and
// is returned for the tick to note.
func (e *Engine) reloadIfHeld(s State) error {
	if s.Hold == nil || e.Reload == nil {
		return nil
	}
	next, err := e.Reload()
	if err != nil {
		return err
	}
	e.Settings = next
	return nil
}

// noteReloadFailure notes a failed reload; a nil error is nothing.
func (t *tick) noteReloadFailure(err error) {
	if err != nil {
		t.note("", "can't read missions.yaml: %v", err)
	}
}
