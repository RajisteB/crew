package mission

import (
	"slices"
	"testing"
)

func TestInputsFor(t *testing.T) {
	m := Mission{Pieces: []Piece{
		{ID: "P1"},
		{ID: "P3", Delegate: true, WaitsFor: []string{"P1"}},
		{ID: "P3.1"},
		{ID: "P3.2", Delegate: true},
		{ID: "P3.2.1", WaitsFor: []string{"P3.1"}},
		{ID: "P4", WaitsFor: []string{"P3"}},
		{ID: "P5", Delegate: true, WaitsFor: []string{"P1"}},
	}}
	cases := []struct {
		id   string
		want []string
	}{
		{"P4", []string{"P3.1", "P3.2.1"}},
		{"P3.2.1", []string{"P1", "P3.1"}}, // its own wait and its ancestor's, in file order
		{"P3.1", []string{"P1"}},
		{"P1", nil},
		{"P5", []string{"P1"}},
	}
	for _, c := range cases {
		if got := InputsFor(m, c.id); !slices.Equal(got, c.want) {
			t.Errorf("InputsFor(%s) = %v, want %v", c.id, got, c.want)
		}
	}
}

func TestInputsFor_DelegatedWithNothingUnderYieldsNothing(t *testing.T) {
	m := Mission{Pieces: []Piece{
		{ID: "P1", Delegate: true},
		{ID: "P2", WaitsFor: []string{"P1"}},
	}}
	if got := InputsFor(m, "P2"); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestInputsFor_DropsItselfAndRepeats(t *testing.T) {
	m := Mission{Pieces: []Piece{
		{ID: "P1", Delegate: true, WaitsFor: []string{"P1.1"}},
		{ID: "P1.1", WaitsFor: []string{"P1.1", "P1.2"}},
		{ID: "P1.2"},
	}}
	if got := InputsFor(m, "P1.1"); !slices.Equal(got, []string{"P1.2"}) {
		t.Fatalf("got %v", got)
	}
}
