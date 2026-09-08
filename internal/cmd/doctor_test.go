package cmd

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/emmmdty/token-usage/internal/config"
	"github.com/emmmdty/token-usage/internal/provider"
)

// doctorOverrides isolates doctor from the real user environment: the config
// path points at a temp file and the network probe is stubbed. It returns a
// restore func to defer.
func doctorOverrides(t *testing.T, probe func() error) {
	t.Helper()
	origGetConfigPath := getConfigPath
	getConfigPath = func() (string, error) {
		return filepath.Join(t.TempDir(), "config.yaml"), nil
	}
	origProbe := doctorNetworkProbe
	doctorNetworkProbe = probe
	origNoColor := noColor
	noColor = true
	t.Cleanup(func() {
		getConfigPath = origGetConfigPath
		doctorNetworkProbe = origProbe
		noColor = origNoColor
	})
}

// A FAILing check must surface as an error (non-zero exit code), so scripts
// and CI can act on doctor's verdict.
func TestDoctorFailingCheckReturnsError(t *testing.T) {
	doctorOverrides(t, func() error { return errors.New("connection refused") })

	if err := doctorCmd.RunE(doctorCmd, []string{}); err == nil {
		t.Fatal("expected doctor to return an error when a check FAILs")
	}
}

// WARN is informational: a warning-only run must not report failure.
func TestDoctorWarningsDoNotFail(t *testing.T) {
	doctorOverrides(t, func() error { return nil })

	// The keyring check degrades to WARN in sandboxes; that must be
	// tolerated while the network check (and config load) succeed.
	if err := doctorCmd.RunE(doctorCmd, []string{}); err != nil {
		t.Fatalf("expected no error for a warning-only run, got: %v", err)
	}
}

// stubArkcliSso replaces the arkcli probes used by checkArkcliSso and
// returns a restore func to defer.
func stubArkcliSso(available bool, status provider.ArkcliHomeStatus, err error) func() {
	origAvail := doctorArkcliAvailable
	origStatus := doctorArkcliHomeStatus
	doctorArkcliAvailable = func() bool { return available }
	doctorArkcliHomeStatus = func(string, string) (provider.ArkcliHomeStatus, error) { return status, err }
	return func() {
		doctorArkcliAvailable = origAvail
		doctorArkcliHomeStatus = origStatus
	}
}

func volcengineTestCfg(enabled bool) *config.Config {
	return &config.Config{
		Providers: map[string]config.PresetProvider{
			"volcengine": {
				Enabled: enabled,
				Accounts: map[string]config.Account{
					"coding-plan":   {Source: "local", Plan: "coding", ArkcliHome: "/homes/one"},
					"coding-plan-2": {Source: "local", Plan: "coding", ArkcliHome: "/homes/two"},
				},
			},
		},
	}
}

// An expired volc-sso login must surface as one WARN per account, carrying
// the exact re-login command with the account's own HOME.
func TestCheckArkcliSsoWarnsOnExpired(t *testing.T) {
	defer stubArkcliSso(true, provider.ArkcliHomeStatus{LoggedIn: true, SsoExpired: true}, nil)()

	checks := checkArkcliSso(volcengineTestCfg(true), nil)
	if len(checks) != 2 {
		t.Fatalf("expected one WARN per account, got %d: %+v", len(checks), checks)
	}
	for _, c := range checks {
		if c.status != "WARN" {
			t.Errorf("expected WARN, got %q", c.status)
		}
		if !containsHomeAndRelogin(c.detail) {
			t.Errorf("detail must carry HOME and re-login command: %q", c.detail)
		}
	}
}

// Healthy logins and disabled/unavailable setups must stay silent.
func TestCheckArkcliSsoSilentWhenHealthy(t *testing.T) {
	defer stubArkcliSso(true, provider.ArkcliHomeStatus{LoggedIn: true}, nil)()

	if checks := checkArkcliSso(volcengineTestCfg(true), nil); len(checks) != 0 {
		t.Errorf("healthy logins must add no checks, got %+v", checks)
	}
	if checks := checkArkcliSso(volcengineTestCfg(false), nil); len(checks) != 0 {
		t.Errorf("disabled provider must add no checks, got %+v", checks)
	}
	defer stubArkcliSso(false, provider.ArkcliHomeStatus{}, nil)()
	if checks := checkArkcliSso(volcengineTestCfg(true), nil); len(checks) != 0 {
		t.Errorf("no arkcli must add no checks, got %+v", checks)
	}
}

// An AK/SK profile queries via its permanent access key, so the
// identity-level sso_expired flag must not trigger a warning.
func TestCheckArkcliSsoSilentForAksk(t *testing.T) {
	defer stubArkcliSso(true, provider.ArkcliHomeStatus{LoggedIn: true, SsoExpired: true, AuthMethod: "aksk"}, nil)()

	if checks := checkArkcliSso(volcengineTestCfg(true), nil); len(checks) != 0 {
		t.Errorf("aksk profiles must add no checks even when sso_expired, got %+v", checks)
	}
}

// A whoami failure (e.g. HOME never logged in) must warn, not crash.
func TestCheckArkcliSsoWarnsOnWhoamiFailure(t *testing.T) {
	defer stubArkcliSso(true, provider.ArkcliHomeStatus{}, errors.New("exit status 1"))()

	checks := checkArkcliSso(volcengineTestCfg(true), nil)
	if len(checks) != 2 {
		t.Fatalf("expected one WARN per account, got %d: %+v", len(checks), checks)
	}
	if checks[0].status != "WARN" {
		t.Errorf("expected WARN, got %q", checks[0].status)
	}
}

func containsHomeAndRelogin(detail string) bool {
	for _, marker := range []string{"HOME=/homes/", "arkcli auth login volc-sso"} {
		if !strings.Contains(detail, marker) {
			return false
		}
	}
	return true
}
