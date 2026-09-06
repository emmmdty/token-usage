package cmd

import (
	"strings"
	"testing"

	"github.com/emmmdty/token-usage/internal/config"
)

func TestDisplayName(t *testing.T) {
	cases := []struct {
		id, plan string
		want     string
	}{
		{"opencode", "", "OpenCode Go"},
		{"claude", "", "Claude"},
		{"codex", "", "Codex"},
		{"volcengine", "coding", "Volcano Engine (Coding Plan)"},
		{"volcengine", "agent", "Volcano Engine (Agent Plan)"},
		{"volcengine", "", "Volcano Engine (Coding Plan)"},
	}
	for _, c := range cases {
		if got := displayName(c.id, c.plan, nil); got != c.want {
			t.Errorf("displayName(%q, %q) = %q, want %q", c.id, c.plan, got, c.want)
		}
	}
	if got := displayName("my-glm", "", &config.CustomProvider{DisplayName: "GLM Coding"}); got != "GLM Coding" {
		t.Errorf("custom display name = %q", got)
	}
}

func TestResolveProviderAccount(t *testing.T) {
	cfg := config.DefaultTestConfig()
	cfg.Providers["opencode"].Accounts["work"] = config.Account{Source: config.SourceManual}
	cfg.Providers["volcengine"].Accounts["coding"] = config.Account{Source: config.SourceLocal, Plan: "coding"}

	// explicit pair
	p, a, err := resolveProviderAccount(cfg, []string{"volcengine", "coding"})
	if err != nil || p != "volcengine" || a != "coding" {
		t.Errorf("explicit pair failed: %s/%s err=%v", p, a, err)
	}

	// slash form
	p, a, err = resolveProviderAccount(cfg, []string{"volcengine/coding"})
	if err != nil || p != "volcengine" || a != "coding" {
		t.Errorf("slash form failed: %s/%s err=%v", p, a, err)
	}

	// unique bare account name
	p, a, err = resolveProviderAccount(cfg, []string{"work"})
	if err != nil || p != "opencode" || a != "work" {
		t.Errorf("bare name failed: %s/%s err=%v", p, a, err)
	}

	// missing
	if _, _, err := resolveProviderAccount(cfg, []string{"nope"}); err == nil {
		t.Error("expected error for missing account")
	}
}

func TestBuildTargetsSkipsDisabledAndMissing(t *testing.T) {
	isolateHome(t)
	cfg := config.DefaultTestConfig()
	cfg.Providers["opencode"].Accounts["work"] = config.Account{Source: config.SourceManual}
	volc := cfg.Providers["volcengine"]
	volc.Enabled = true
	volc.Accounts = map[string]config.Account{
		"coding": {Source: config.SourceLocal, Plan: "coding"},
		"agent":  {Source: config.SourceLocal, Plan: "agent"},
	}
	cfg.Providers["volcengine"] = volc

	// With an isolated HOME no local credential files exist and no stored
	// keys are present, so every target must be skipped with a note.
	targets, notes, err := buildTargets(cfg, "", "")
	if err != nil {
		t.Fatalf("buildTargets failed: %v", err)
	}
	if len(targets) != 0 {
		t.Errorf("expected no queryable targets in isolated test env, got %d", len(targets))
	}
	if len(notes) == 0 {
		t.Error("expected notes explaining skipped targets")
	}
	for _, n := range notes {
		if !strings.Contains(n, "/") {
			t.Errorf("note should reference provider/account: %q", n)
		}
	}
}

func TestBuildTargetsHonorsFilters(t *testing.T) {
	isolateHome(t)
	cfg := config.DefaultTestConfig()
	volc := cfg.Providers["volcengine"]
	volc.Enabled = true
	volc.Accounts = map[string]config.Account{
		"coding": {Source: config.SourceLocal, Plan: "coding"},
		"agent":  {Source: config.SourceLocal, Plan: "agent"},
	}
	cfg.Providers["volcengine"] = volc

	_, notes, _ := buildTargets(cfg, "volcengine", "agent")
	if len(notes) != 1 || !strings.Contains(notes[0], "volcengine/agent") {
		t.Errorf("expected only volcengine/agent note, got %v", notes)
	}

	_, notes, _ = buildTargets(cfg, "volcengine", "")
	if len(notes) != 2 {
		t.Errorf("expected both volcengine accounts, got %v", notes)
	}
}

func TestVolcenginePlanProfile(t *testing.T) {
	cfg := config.DefaultTestConfig()
	volc := cfg.Providers["volcengine"]
	volc.Accounts = map[string]config.Account{
		"coding-plan": {Source: config.SourceLocal, Plan: "coding"},
		"phone2":      {Source: config.SourceArkcli, Plan: "agent", Profile: "p2"},
		"bare":        {Source: config.SourceArkcli},
	}
	cfg.Providers["volcengine"] = volc

	plan, profile := volcenginePlanProfile(cfg, "phone2")
	if plan != "agent" || profile != "p2" {
		t.Errorf("phone2 = (%q, %q), want (agent, p2)", plan, profile)
	}
	plan, profile = volcenginePlanProfile(cfg, "bare")
	if plan != "coding" || profile != "" {
		t.Errorf("bare = (%q, %q), want (coding, \"\")", plan, profile)
	}
	plan, profile = volcenginePlanProfile(cfg, "missing")
	if plan != "coding" || profile != "" {
		t.Errorf("missing = (%q, %q), want (coding, \"\")", plan, profile)
	}
}

func TestBuildTargetsVolcengineArkcliSource(t *testing.T) {
	isolateHome(t)
	cfg := config.DefaultTestConfig()
	volc := cfg.Providers["volcengine"]
	volc.Enabled = true
	// arkcli-source accounts carry no stored key: the arkcli login state is
	// the credential, so both targets must build without any notes.
	volc.Accounts = map[string]config.Account{
		"phone1": {Source: config.SourceArkcli, Plan: "coding", Profile: "p1"},
		"phone2": {Source: config.SourceArkcli, Plan: "coding", Profile: "p2"},
	}
	cfg.Providers["volcengine"] = volc

	targets, notes, err := buildTargets(cfg, "volcengine", "")
	if err != nil {
		t.Fatalf("buildTargets failed: %v", err)
	}
	if len(notes) != 0 {
		t.Errorf("expected no notes for arkcli-source accounts, got %v", notes)
	}
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(targets))
	}
	for _, want := range []string{"phone1", "phone2"} {
		found := false
		for _, tg := range targets {
			if tg.Account == want {
				found = true
			}
		}
		if !found {
			t.Errorf("missing target for account %q in %+v", want, targets)
		}
	}
}

// isolateHome points the HOME/USERPROFILE env vars at a fresh temp dir so
// tests never read real user credentials.
func isolateHome(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}
