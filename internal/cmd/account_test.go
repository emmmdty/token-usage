package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func writeVolcengineOpencodeJSON(t *testing.T, entries map[string]string) string {
	t.Helper()
	prov := map[string]interface{}{}
	for id, key := range entries {
		prov[id] = map[string]interface{}{"options": map[string]string{"apiKey": key}}
	}
	data, err := json.Marshal(map[string]interface{}{"provider": prov})
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	path := filepath.Join(t.TempDir(), "opencode.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	return path
}

func TestVolcengineOpencodeEntries(t *testing.T) {
	path := writeVolcengineOpencodeJSON(t, map[string]string{
		"Volcano-Engine-coding-plan-2": "ark-key-2",
		"Volcano-Engine-coding-plan":   "ark-key-1",
		"openai":                       "", // skipped: no key
	})
	entries, err := volcengineOpencodeEntries(path)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(entries), entries)
	}
	// Deterministic order, independent of JSON map order.
	if entries[0].ID != "Volcano-Engine-coding-plan" || entries[0].Key != "ark-key-1" {
		t.Errorf("unexpected first entry: %+v", entries[0])
	}
	if entries[1].ID != "Volcano-Engine-coding-plan-2" || entries[1].Key != "ark-key-2" {
		t.Errorf("unexpected second entry: %+v", entries[1])
	}

	if _, err := volcengineOpencodeEntries(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Error("expected error for missing file")
	}
}

func TestVolcengineKeyForEntry(t *testing.T) {
	path := writeVolcengineOpencodeJSON(t, map[string]string{
		"Volcano-Engine-coding-plan":   "ark-key-1",
		"Volcano-Engine-coding-plan-2": "ark-key-2",
	})

	// Explicit binding wins.
	key, err := volcengineKeyForEntry(path, "Volcano-Engine-coding-plan-2")
	if err != nil || key != "ark-key-2" {
		t.Errorf("pinned entry = %q err=%v, want ark-key-2", key, err)
	}
	// Unknown binding errors instead of silently reading another account.
	if _, err := volcengineKeyForEntry(path, "nope"); err == nil {
		t.Error("expected error for unknown entry binding")
	}
	// Legacy fallback without binding.
	key, err = volcengineKeyForEntry(path, "")
	if err != nil || key != "ark-key-1" {
		t.Errorf("legacy fallback = %q err=%v, want ark-key-1", key, err)
	}
}

func TestDeriveVolcengineAccountName(t *testing.T) {
	cases := map[string]string{
		"Volcano-Engine-coding-plan":   "coding-plan",
		"Volcano-Engine-coding-plan-2": "coding-plan-2",
		"weird/id:name":                "weird-id-name",
		"":                             "local",
	}
	for id, want := range cases {
		if got := deriveVolcengineAccountName(id); got != want {
			t.Errorf("derive(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestAddVolcengineLocalAccounts(t *testing.T) {
	isolateHome(t)
	opencodeJSON := writeVolcengineOpencodeJSON(t, map[string]string{
		"Volcano-Engine-coding-plan":   "ark-key-1",
		"Volcano-Engine-coding-plan-2": "ark-key-2",
	})
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.DefaultTestConfig()
	p := cfg.Providers["volcengine"]
	p.Enabled = true
	p.OpencodeJSON = opencodeJSON
	cfg.Providers["volcengine"] = p

	// Non-interactive "add all": stdin answers 'y' to the add-all prompt.
	restoreStdin := replaceStdin(t, "y\n")
	defer restoreStdin()
	if err := addVolcengineLocalAccounts(cfg, cfgPath, cfg.Providers["volcengine"], "coding", "", addOpts{}); err != nil {
		t.Fatalf("addVolcengineLocalAccounts failed: %v", err)
	}

	got := cfg.Providers["volcengine"]
	if len(got.Accounts) != 2 {
		t.Fatalf("expected 2 accounts, got %d: %+v", len(got.Accounts), got.Accounts)
	}
	a1 := got.Accounts["coding-plan"]
	a2 := got.Accounts["coding-plan-2"]
	if a1.OpencodeProvider != "Volcano-Engine-coding-plan" {
		t.Errorf("coding-plan binding = %q", a1.OpencodeProvider)
	}
	if a2.OpencodeProvider != "Volcano-Engine-coding-plan-2" {
		t.Errorf("coding-plan-2 binding = %q", a2.OpencodeProvider)
	}
	if a1.Source != config.SourceLocal || a2.Plan != "coding" {
		t.Errorf("unexpected accounts: %+v %+v", a1, a2)
	}
	if got.DefaultAccount != "coding-plan" {
		t.Errorf("default account = %q, want coding-plan", got.DefaultAccount)
	}

	// Re-adding with the same name refreshes the binding in place.
	if err := addVolcengineLocalAccounts(cfg, cfgPath, got, "coding", "", addOpts{name: "coding-plan-2", localRef: "Volcano-Engine-coding-plan-2"}); err != nil {
		t.Fatalf("re-add failed: %v", err)
	}
	got = cfg.Providers["volcengine"]
	if len(got.Accounts) != 2 {
		t.Errorf("expected still 2 accounts, got %d", len(got.Accounts))
	}
	if acc := got.Accounts["coding-plan-2"]; acc.OpencodeProvider != "Volcano-Engine-coding-plan-2" || acc.CreatedAt.IsZero() {
		t.Errorf("coding-plan-2 binding = %q created=%v", acc.OpencodeProvider, acc.CreatedAt)
	}

	// A second account on an already-bound entry is refused (it would
	// render identical quota rows).
	if err := addVolcengineLocalAccounts(cfg, cfgPath, got, "coding", "", addOpts{name: "phone2", localRef: "Volcano-Engine-coding-plan-2"}); err != nil {
		t.Fatalf("duplicate bind should skip, not fail: %v", err)
	}
	if _, ok := cfg.Providers["volcengine"].Accounts["phone2"]; ok {
		t.Error("entry already bound by coding-plan-2; phone2 must not be created")
	}
	if got.Accounts["coding-plan"].OpencodeProvider != "Volcano-Engine-coding-plan" {
		t.Errorf("coding-plan binding was clobbered: %+v", got.Accounts["coding-plan"])
	}
}

// replaceStdin feeds s to os.Stdin for the duration of the test (the add
// flows read prompts from os.Stdin directly).
func replaceStdin(t *testing.T, s string) func() {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe failed: %v", err)
	}
	if _, err := w.WriteString(s); err != nil {
		t.Fatalf("stdin write failed: %v", err)
	}
	w.Close()
	old := os.Stdin
	os.Stdin = r
	return func() { os.Stdin = old; r.Close() }
}
