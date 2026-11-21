package mission

import (
	"context"
	"time"
)

// restartDelay is the pause between a crash and the next start.
var restartDelay = time.Second

// Supervise runs start until it returns nil or ctx ends. Any other error
// calls onCrash(n, err) and starts again, up to max restarts; after that it
// returns the last error. A cancelled ctx returns nil. onCrash may be nil.
func Supervise(ctx context.Context, start func(ctx context.Context) error, max int, onCrash func(n int, err error)) error {
	for crashes := 0; ; {
		err := start(ctx)
		if err == nil || ctx.Err() != nil {
			return nil
		}
		if crashes >= max {
			return err
		}
		crashes++
		if onCrash != nil {
			onCrash(crashes, err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(restartDelay):
		}
	}
}
