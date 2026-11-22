package mission

import "testing"

func TestCheck_EpicIsAReservedID(t *testing.T) {
	for _, id := range []string{"epic", "Epic", "EPIC"} {
		m := acme()
		m.Pieces[2].ID = id
		errs := Check(m, baseInput())
		found := false
		for _, e := range errs {
			found = found || e.Error() == `piece id "epic" is reserved`
		}
		if !found {
			t.Errorf("%s: errors = %v", id, errs)
		}
	}
	m := acme()
	m.Pieces[2].ID = "epic-2"
	if errs := Check(m, baseInput()); len(errs) != 0 {
		t.Errorf("epic-2: errors = %v", errs)
	}
}
