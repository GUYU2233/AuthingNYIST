package app

import "sync"

// Password storage backends reported by PasswordStorage.
const (
	// PasswordBackendFile means the password was written to config.json using
	// the platform's file protection (DPAPI on Windows, a 0600 file elsewhere).
	PasswordBackendFile = "file"
	// PasswordBackendKeyring means only a keyring:v1 reference was written to
	// config.json; the password itself lives in the Secret Service.
	PasswordBackendKeyring = "keyring"
)

var passwordBackend = struct {
	sync.RWMutex
	name string
}{name: PasswordBackendFile}

// PasswordStorage reports where the most recent Save put the password:
// PasswordBackendKeyring when it was stored in the freedesktop Secret Service,
// or PasswordBackendFile when Save had to fall back to the platform's normal
// file storage. Callers can use it to warn that the password is not keyring
// protected. It is safe to call concurrently with Save.
func PasswordStorage() string {
	passwordBackend.RLock()
	defer passwordBackend.RUnlock()
	return passwordBackend.name
}

func setPasswordStorage(name string) {
	passwordBackend.Lock()
	passwordBackend.name = name
	passwordBackend.Unlock()
}
