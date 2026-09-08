//go:build !windows

package auth

import (
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

var secretsMu sync.Mutex

// withSecretsLock runs fn while holding an advisory flock on a lock file next
// to secrets.enc, serializing read-modify-write cycles across processes. If
// the lock file cannot be created or locked, it degrades to in-process
// serialization only.
func withSecretsLock(fn func() error) error {
	secretsMu.Lock()
	defer secretsMu.Unlock()

	path, err := getEncryptedPath()
	if err != nil {
		return fn()
	}

	lockPath := path + ".lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), 0700); err != nil {
		return fn()
	}

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fn()
	}
	defer func() { _ = f.Close() }()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fn()
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:errcheck

	return fn()
}
