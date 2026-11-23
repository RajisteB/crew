package mission

// scopeSummary gives the summary to the orchestrator scoped to asker, the
// delegated piece AskerOf named; "" leaves it for the main orchestrator.
func (t *tick) scopeSummary(sum Summary, asker string) Summary {
	if asker == "" {
		return sum
	}
	parent, _ := t.m.Piece(asker)
	return sum.Scoped(t.m, asker, t.e.Domains[parent.Domain])
}
