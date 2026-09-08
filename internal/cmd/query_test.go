package cmd

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/emmmdty/token-usage/internal/config"
	"github.com/emmmdty/token-usage/internal/provider"
)

// One wedged target must not hold the whole report hostage: after the
// overall deadline the run completes, with the unfinished target reported
// as a timeout error.
func TestRunQueryTargetsOverallTimeout(t *testing.T) {
	origTimeout := queryOverallTimeout
	queryOverallTimeout = 100 * time.Millisecond
	t.Cleanup(func() { queryOverallTimeout = origTimeout })

	slow := queryTarget{
		ProviderID: "slow",
		Account:    "a",
		Name:       "slow",
		query: func() (*provider.Usage, error) {
			time.Sleep(2 * time.Second)
			return &provider.Usage{Provider: "slow"}, nil
		},
	}
	fast := queryTarget{
		ProviderID: "fast",
		Account:    "a",
		Name:       "fast",
		query: func() (*provider.Usage, error) {
			return &provider.Usage{Provider: "fast"}, nil
		},
	}

	start := time.Now()
	results := runQueryTargets(config.DefaultTestConfig(), []queryTarget{slow, fast})
	elapsed := time.Since(start)

	if elapsed >= time.Second {
		t.Fatalf("run took %v; the overall timeout should have cut it off at ~100ms", elapsed)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Results must stay in the stable target order regardless of timeouts.
	if results[0].Target.ProviderID != "slow" || results[1].Target.ProviderID != "fast" {
		t.Fatalf("results out of order: %s, %s", results[0].Target.ProviderID, results[1].Target.ProviderID)
	}

	if results[0].Err == nil || !strings.Contains(results[0].Err.Error(), "timed out") {
		t.Errorf("slow target: expected a timeout error, got %+v", results[0].Err)
	}
	if results[1].Err != nil || results[1].Usage == nil {
		t.Errorf("fast target: expected a successful result, got err=%v usage=%v", results[1].Err, results[1].Usage)
	}
}

// A normal (non-wedged) run must be unaffected by the deadline machinery.
func TestRunQueryTargetsNoTimeoutOnHealthyRun(t *testing.T) {
	targets := make([]queryTarget, 10)
	for i := range targets {
		targets[i] = queryTarget{
			ProviderID: "p",
			Account:    string(rune('a' + i)),
			Name:       "p",
			query: func() (*provider.Usage, error) {
				return &provider.Usage{Provider: "p"}, nil
			},
		}
	}
	results := runQueryTargets(config.DefaultTestConfig(), targets)
	if len(results) != len(targets) {
		t.Fatalf("expected %d results, got %d", len(targets), len(results))
	}
	for _, r := range results {
		if r.Err != nil {
			t.Errorf("unexpected error on healthy run: %v", r.Err)
		}
	}
}

// touchLastVerified must not silently drop persistence failures: the
// last_verified stamp is diagnostic data and a failed save deserves a
// warning on stderr.
func TestTouchLastVerifiedWarnsOnSaveFailure(t *testing.T) {
	cfg := config.DefaultTestConfig()
	dirPath := t.TempDir()

	stderr := captureStderr(t, func() {
		touchLastVerified(cfg, "opencode", "nonexistent", dirPath)
	})
	if stderr == "" {
		t.Error("expected a warning when last_verified cannot be persisted")
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stderr = w
	fn()
	_ = w.Close()
	os.Stderr = orig
	data, _ := io.ReadAll(r)
	return string(data)
}
