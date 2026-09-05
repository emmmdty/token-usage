package cmd

import (
	"os"
	"testing"
)

// TestMain keeps the cmd-layer tests off the real system keyring: probing
// it (SecItemAdd on macOS, Secret Service on Linux) can block or prompt in
// CI and on developer machines. With the variable set, credential
// operations deterministically use the encrypted-file backend.
func TestMain(m *testing.M) {
	os.Setenv("TOKEN_USAGE_KEYRING_DISABLED", "1")
	os.Exit(m.Run())
}
