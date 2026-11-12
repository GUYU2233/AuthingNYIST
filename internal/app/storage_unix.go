//go:build !windows
// +build !windows

package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const keyringPrefix = "keyring:v1:"

// keyringTimeout bounds every secret-tool invocation so a hung or absent
// session bus cannot block saving or loading a configuration indefinitely.
const keyringTimeout = 5 * time.Second

// passwordKeyID derives the Secret Service account name for a configuration
// file. It is a deterministic, non-secret reference so config.json never needs
// to contain the password. Hashing the absolute config path keeps separate
// profiles (and test directories) in separate keyring entries; only the first
// 16 hex characters of SHA-256 are kept, which is plenty to avoid collisions
// between a handful of local config files. The username is deliberately not
// part of the ID: Save always overwrites the entry, so changing the account
// stores the new password in the same path-scoped slot instead of orphaning it.
func passwordKeyID(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = filepath.Clean(path)
	}
	sum := sha256.Sum256([]byte(abs))
	return hex.EncodeToString(sum[:])[:16]
}

func validPasswordID(id string) bool {
	if len(id) != 16 {
		return false
	}
	for _, c := range id {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// protectPassword stores s in the Secret Service when possible and returns a
// keyring:v1 reference. Any keyring failure falls back to returning s itself so
// saving a configuration never fails because a keyring is unavailable. The
// config path scopes the deterministic keyring account name.
func protectPassword(path, s string) (string, error) {
	if s == "" {
		setPasswordStorage(PasswordBackendFile)
		return "", nil
	}
	id := passwordKeyID(path)
	if err := keyringStore(id, s); err == nil {
		setPasswordStorage(PasswordBackendKeyring)
		return keyringPrefix + id, nil
	}
	setPasswordStorage(PasswordBackendFile)
	return s, nil
}

func unprotectPassword(s string) (string, error) {
	if strings.HasPrefix(s, "dpapi:") {
		return "", errors.New("Windows-protected password cannot be loaded here; enter it again")
	}
	if strings.HasPrefix(s, keyringPrefix) {
		id := strings.TrimPrefix(s, keyringPrefix)
		if !validPasswordID(id) {
			return "", errors.New("invalid keyring password reference; enter the password again")
		}
		p, err := keyringLookup(id)
		if err != nil {
			return "", errors.New("stored password is unavailable in the system keyring; enter the password again")
		}
		return p, nil
	}
	return s, nil
}

func keyringTimeoutContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), keyringTimeout)
}

func secureDirectory(path string) error { return os.Chmod(path, 0700) }
func replaceFile(from, to string) error { return os.Rename(from, to) }
