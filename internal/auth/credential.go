package auth

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/99designs/keyring"
	"github.com/emmmdty/token-usage/internal/i18n"
)

var (
	keyringOnce      sync.Once
	ring             keyring.Keyring
	keyringAvailable bool
	// keyringDisabled lets tests force the encrypted-file backend without
	// touching a real system keyring.
	keyringDisabled bool
)

// keyringDisabledByEnv reports whether the system keyring probe should be
// skipped entirely (headless/CI environments where a real keychain is
// unavailable or can block on a security prompt).
func keyringDisabledByEnv() bool {
	return os.Getenv("TOKEN_USAGE_KEYRING_DISABLED") != ""
}

// ensureKeyring lazily opens the system keyring and probes it with a
// write/read cycle. Keeping this out of init() makes importing the package
// side-effect free (important for tests and headless environments).
func ensureKeyring() {
	keyringOnce.Do(func() {
		if keyringDisabled || keyringDisabledByEnv() {
			return
		}

		homeDir, err := os.UserHomeDir()
		if err != nil {
			return
		}

		cfg := keyring.Config{
			ServiceName: "token-usage",
			FilePasswordFunc: func(prompt string) (string, error) {
				if pwd := os.Getenv("TOKEN_USAGE_KEYRING_PASSWORD"); pwd != "" {
					return pwd, nil
				}
				return "", errors.New(i18n.T("error.auth.keyring_password"))
			},
		}
		cfg.FileDir = filepath.Join(homeDir, ".config", "token-usage", "keyring")

		r, err := keyring.Open(cfg)
		if err != nil {
			return
		}

		testKey := "__token_usage_test__"
		if err := r.Set(keyring.Item{Key: testKey, Data: []byte("test")}); err != nil {
			return
		}
		_ = r.Remove(testKey)

		ring = r
		keyringAvailable = true
	})
}

func IsKeyringAvailable() bool {
	ensureKeyring()
	return keyringAvailable
}

// ErrKeyNotFound reports that no key is stored for the account. It is
// returned by GetAPIKey regardless of the backing store so callers can
// distinguish "missing" from a genuine store failure.
var ErrKeyNotFound = errors.New("API key not found")

func StoreAPIKey(service, account, apiKey string) error {
	ensureKeyring()
	if ring != nil {
		return ring.Set(keyring.Item{
			Key:  account,
			Data: []byte(apiKey),
		})
	}
	return storeEncrypted(account, apiKey)
}

func GetAPIKey(service, account string) (string, error) {
	ensureKeyring()
	if ring != nil {
		item, err := ring.Get(account)
		if err != nil {
			if errors.Is(err, keyring.ErrKeyNotFound) {
				return "", fmt.Errorf("%w for account: %s", ErrKeyNotFound, account)
			}
			return "", fmt.Errorf("%s", i18n.T("error.auth.keyring_error", err))
		}
		return string(item.Data), nil
	}
	key, err := getEncrypted(account)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return "", fmt.Errorf("%w for account: %s", ErrKeyNotFound, account)
		}
		return "", err
	}
	return key, nil
}

func DeleteAPIKey(service, account string) error {
	ensureKeyring()
	if ring != nil {
		err := ring.Remove(account)
		if err != nil && errors.Is(err, keyring.ErrKeyNotFound) {
			return nil
		}
		return err
	}
	return deleteEncrypted(account)
}

// ListAccountKeys returns all stored account key names. Best-effort: on
// failure it returns whatever is available (possibly nothing) instead of an
// error, since callers use it for reconciliation/display only.
func ListAccountKeys() []string {
	ensureKeyring()
	if ring != nil {
		keys, err := ring.Keys()
		if err != nil {
			return nil
		}
		out := make([]string, 0, len(keys))
		for _, k := range keys {
			if strings.HasPrefix(k, "__") {
				continue // internal probe keys
			}
			out = append(out, k)
		}
		return out
	}
	secrets, err := loadEncrypted()
	if err != nil || secrets == nil {
		return nil
	}
	out := make([]string, 0, len(secrets))
	for k := range secrets {
		out = append(out, k)
	}
	return out
}

// ExtractKeyID returns the last 6 characters (rune-safe) of the key for
// display and matching; keys of 6 or fewer characters are returned as-is.
func ExtractKeyID(apiKey string) string {
	runes := []rune(apiKey)
	if len(runes) > 6 {
		return string(runes[len(runes)-6:])
	}
	return apiKey
}
