package mission

import (
	"context"
	"errors"
	"testing"
)

func TestSupervise_RestartsUpToMaxThenReturnsError(t *testing.T) {
	restartDelay = 0
	starts, crashes := 0, 0
	last := errors.New("crash 4")
	err := Supervise(context.Background(), func(context.Context) error {
		starts++
		if starts == 4 {
			return last
		}
		return errors.New("crash")
	}, 3, func(n int, err error) { crashes++ })
	if starts != 4 || crashes != 3 || !errors.Is(err, last) {
		t.Fatalf("starts = %d crashes = %d err = %v", starts, crashes, err)
	}
}

func TestSupervise_CleanExitDoesNotRestart(t *testing.T) {
	restartDelay = 0
	starts := 0
	err := Supervise(context.Background(), func(context.Context) error { starts++; return nil }, 3, nil)
	if err != nil || starts != 1 {
		t.Fatalf("starts = %d err = %v", starts, err)
	}
}

func TestSupervise_CancelDoesNotRestart(t *testing.T) {
	restartDelay = 0
	ctx, cancel := context.WithCancel(context.Background())
	starts, crashes := 0, 0
	err := Supervise(ctx, func(context.Context) error {
		starts++
		cancel()
		return errors.New("stopped")
	}, 3, func(int, error) { crashes++ })
	if starts != 1 || crashes != 0 || err != nil {
		t.Fatalf("starts = %d crashes = %d err = %v", starts, crashes, err)
	}
}
