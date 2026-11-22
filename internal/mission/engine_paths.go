package mission

// observeOutside lists the files a done piece with commits ahead changed
// outside its effective paths. ok is false when they could not be listed: that
// is a note, and the piece is left out of the observation.
func (t *tick) observeOutside(p PieceState, ahead int) (outside []string, ok bool) {
	piece, _, found := t.pieceMission(p.ID)
	if !found {
		t.note(p.ID, "can't check the files the piece changed: the piece is not in the mission")
		return nil, false
	}
	if ahead == 0 {
		return nil, true
	}
	paths := EffectivePaths(piece, t.e.Domains[piece.Domain])
	if len(paths) == 0 {
		return nil, true
	}
	files, err := t.e.Git.ChangedFiles(t.ctx, p.Worktree, "origin/"+t.s.Base)
	if err != nil {
		t.note(p.ID, "can't list the files the piece changed: %v", err)
		return nil, false
	}
	return Outside(paths, files), true
}
