package sending

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type ownershipLock struct{ file *os.File }

func acquireOwnership(dir string) (*ownershipLock, error) {
	if dir == "" {
		return nil, fmt.Errorf("dispatcher data directory is required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, ".sending.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, errors.Join(fmt.Errorf("another dispatcher owns this data directory: %w", err), f.Close())
	}
	return &ownershipLock{file: f}, nil
}

// LockDataDirectory acquires exclusive dispatcher ownership of dir without
// starting a dispatcher. Destructive local maintenance uses it to prove that no
// worker owns the data before anything is removed.
func LockDataDirectory(dir string) (func() error, error) {
	lock, err := acquireOwnership(dir)
	if err != nil {
		return nil, err
	}
	return lock.Close, nil
}

func (l *ownershipLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := unix.Flock(int(l.file.Fd()), unix.LOCK_UN)
	closeErr := l.file.Close()
	l.file = nil
	if err != nil {
		return err
	}
	return closeErr
}
