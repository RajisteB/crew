// Package flock is the one-driver lock the mission and run stores share: an
// exclusive, non-blocking flock held for the life of the holder, with the
// holder's pid written inside for messages. "Held" means the flock can't be
// taken; a pid left in the file by a process that is gone says nothing.
package flock

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	// A probe (Held) takes the flock for an instant, so one failed attempt
	// doesn't prove the lock is held. Acquire tries a few times before it
	// says so; a lock that is really held still fails within about 50ms.
	acquireAttempts = 5
	acquirePause    = 10 * time.Millisecond
)

// ErrHeld means another open file description holds the lock.
var ErrHeld = errors.New("lock is held")

// Acquire takes the lock on path and writes this process's pid into it. The
// returned func releases it, once. A lock that is held returns an error
// wrapping ErrHeld. The file is never removed, so no process can lock a
// stale inode, and a crashed holder leaves nothing behind that blocks.
func Acquire(path string, mode os.FileMode) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, mode)
	if err != nil {
		return nil, err
	}
	if err := tryLock(f); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Truncate(0); err == nil {
		_, err = f.WriteAt([]byte(strconv.Itoa(os.Getpid())), 0)
	}
	if err != nil {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			f.Truncate(0)
			syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
			f.Close()
		})
	}, nil
}

// tryLock takes the flock without blocking, retrying while it looks held.
func tryLock(f *os.File) error {
	var err error
	for i := range acquireAttempts {
		if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
			return nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return err
		}
		if i < acquireAttempts-1 {
			time.Sleep(acquirePause)
		}
	}
	return ErrHeld
}

// Pid reads the pid written in the lock file; 0 when there is none.
func Pid(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

// Held reports the pid in the lock file and whether some process holds the
// lock right now. If nobody holds the flock, the pid left in the file is not
// a live holder (the process crashed, or its pid was reused).
func Held(path string) (int, bool) {
	pid := Pid(path)
	if pid == 0 {
		return 0, false
	}
	if err := syscall.Kill(pid, 0); err != nil && !errors.Is(err, syscall.EPERM) {
		return pid, false
	}
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return pid, false
	}
	defer f.Close()
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		return pid, false
	}
	return pid, true
}
