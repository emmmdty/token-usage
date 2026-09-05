package test

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// buildCLIBinary compiles the real token-usage binary for end-to-end tests.
func buildCLIBinary(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "token-usage-test")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	buildCmd := exec.Command("go", "build", "-o", binary, "../cmd/token-usage/")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build binary: %v\n%s", err, out)
	}
	return binary
}

// cliRun executes the binary with an isolated HOME and returns stdout,
// stderr and the exit code separately (so stream-sensitive assertions stay
// possible).
func cliRun(t *testing.T, binary string, home string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(binary, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"USERPROFILE="+home,
		"LANG=en_US.UTF-8",
		"LC_ALL=en_US.UTF-8",
		"NO_COLOR=1",
		"TOKEN_USAGE_MASTER_PASSWORD=ci-test-master-password",
		"TOKEN_USAGE_KEYRING_DISABLED=1",
	)
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("failed to run %v: %v\nstdout:\n%s\nstderr:\n%s", args, err, stdout.String(), stderr.String())
		}
		return stdout.String(), stderr.String(), exitErr.ExitCode()
	}
	return stdout.String(), stderr.String(), 0
}

func TestCLIIntegration(t *testing.T) {
	binary := buildCLIBinary(t)

	out, _, code := cliRun(t, binary, t.TempDir(), "--help")
	if code != 0 {
		t.Fatalf("--help exited %d", code)
	}
	if !strings.Contains(out, "Token Usage") {
		t.Error("help output should contain 'Token Usage'")
	}
}

// A fresh install (no accounts, no credentials) must produce a valid empty
// JSON response with exit code 0 — not an error and not `null`.
func TestCLIQuotaJSONFreshHome(t *testing.T) {
	binary := buildCLIBinary(t)

	out, _, code := cliRun(t, binary, t.TempDir(), "quota", "--json")
	if code != 0 {
		t.Fatalf("quota --json exited %d, output:\n%s", code, out)
	}

	var resp struct {
		Version  string            `json:"version"`
		Accounts []json.RawMessage `json:"accounts"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("quota --json did not print valid JSON: %v\n%s", err, out)
	}
	if resp.Version != "2" {
		t.Errorf("expected version 2, got %q", resp.Version)
	}
	if len(resp.Accounts) != 0 {
		t.Errorf("expected empty accounts, got %d entries", len(resp.Accounts))
	}
}

// Filtering by an unknown provider with no configured accounts is an error
// (exit 1) in the human-readable mode.
func TestCLIQuotaUnknownProviderExitsOne(t *testing.T) {
	binary := buildCLIBinary(t)

	out, _, code := cliRun(t, binary, t.TempDir(), "quota", "no-such-provider")
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d, output:\n%s", code, out)
	}
}

func TestCLIProviderListFreshHome(t *testing.T) {
	binary := buildCLIBinary(t)

	out, _, code := cliRun(t, binary, t.TempDir(), "provider", "list")
	if code != 0 {
		t.Fatalf("provider list exited %d, output:\n%s", code, out)
	}
	if !strings.Contains(out, "OpenCode") {
		t.Errorf("expected OpenCode preset in provider list, got:\n%s", out)
	}
}

// tu lang persists the language choice into the config file.
func TestCLILangSwitchPersists(t *testing.T) {
	binary := buildCLIBinary(t)
	home := t.TempDir()

	if _, _, code := cliRun(t, binary, home, "lang", "zh"); code != 0 {
		t.Fatalf("lang zh exited non-zero")
	}

	data, err := os.ReadFile(filepath.Join(home, ".config", "token-usage", "config.yaml"))
	if err != nil {
		t.Fatalf("config.yaml not created under isolated HOME: %v", err)
	}
	if !strings.Contains(string(data), "language: zh") {
		t.Errorf("expected 'language: zh' persisted in config, got:\n%s", data)
	}

	out, _, code := cliRun(t, binary, home, "lang")
	if code != 0 || !strings.Contains(out, "zh") {
		t.Errorf("expected 'lang' to report zh (code=%d), got:\n%s", code, out)
	}
}

// doctor must run all checks without crashing in a fresh environment. Its
// exit code depends on sandbox network access (0 all-pass, 1 on failures),
// but the report itself must always be printed.
func TestCLIDoctorFreshHome(t *testing.T) {
	binary := buildCLIBinary(t)

	out, _, code := cliRun(t, binary, t.TempDir(), "doctor")
	if code != 0 && code != 1 {
		t.Fatalf("doctor exited %d, output:\n%s", code, out)
	}
	if !strings.Contains(out, "Config") {
		t.Errorf("expected config check in doctor output, got:\n%s", out)
	}
}
