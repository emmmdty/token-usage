package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	cfg, err := LoadOrCreateConfig(configPath)
	if err != nil {
		t.Fatalf("failed to create config: %v", err)
	}

	if cfg.Version != CurrentVersion {
		t.Errorf("expected version %s, got %s", CurrentVersion, cfg.Version)
	}

	if cfg.MaxConcurrentRequests != 5 {
		t.Errorf("expected max concurrent requests 5, got %d", cfg.MaxConcurrentRequests)
	}

	if _, ok := cfg.Providers["opencode"]; !ok {
		t.Error("expected opencode preset to exist")
	}
	if _, ok := cfg.Providers["volcengine"]; !ok {
		t.Error("expected volcengine preset to exist")
	}
}

func TestMigrateV2Config(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	v2 := `version: "2"
accounts:
    work:
        name: work
        key_id: "abc123"
        created_at: "2026-08-24T10:00:00Z"
        last_verified: "2026-08-24T10:05:00Z"
providers:
    claude:
        enabled: true
        creds_path: /tmp/creds.json
    codex:
        enabled: true
        auth_path: /tmp/auth.json
    opencode:
        enabled: true
    volcengine:
        enabled: false
color_thresholds:
    warning: 40
    danger: 90
max_concurrent_requests: 3
`
	if err := os.WriteFile(configPath, []byte(v2), 0600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := LoadOrCreateConfig(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.Version != CurrentVersion {
		t.Errorf("expected migrated version %s, got %s", CurrentVersion, cfg.Version)
	}

	oc, ok := cfg.Providers["opencode"]
	if !ok {
		t.Fatal("expected opencode provider")
	}
	acc, exists := oc.Accounts["work"]
	if !exists {
		t.Fatal("expected v2 account 'work' to migrate under providers.opencode")
	}
	if acc.Source != SourceManual || acc.KeyID != "abc123" {
		t.Errorf("unexpected migrated account: %+v", acc)
	}
	if oc.DefaultAccount != "work" {
		t.Errorf("expected default account 'work', got %q", oc.DefaultAccount)
	}

	if cl := cfg.Providers["claude"]; !cl.Enabled || cl.CredsPath != "/tmp/creds.json" {
		t.Errorf("claude provider not migrated correctly: %+v", cl)
	}
	if cx := cfg.Providers["codex"]; !cx.Enabled || cx.AuthPath != "/tmp/auth.json" {
		t.Errorf("codex provider not migrated correctly: %+v", cx)
	}

	if cfg.ColorThresholds.Warning != 40 || cfg.ColorThresholds.Danger != 90 {
		t.Errorf("thresholds not migrated: %+v", cfg.ColorThresholds)
	}
	if cfg.MaxConcurrentRequests != 3 {
		t.Errorf("max concurrent requests not migrated: %d", cfg.MaxConcurrentRequests)
	}
}

func TestFindProvider(t *testing.T) {
	cfg := getDefaultConfig()
	cfg.Custom["my-glm"] = CustomProvider{
		QueryType: "zai-glm",
		BaseURL:   "https://api.z.ai",
		Enabled:   true,
		Accounts:  map[string]Account{"main": {Source: SourceManual}},
	}

	if _, enabled, err := cfg.FindProvider("opencode"); err != nil || !enabled {
		t.Errorf("opencode lookup failed: enabled=%v err=%v", enabled, err)
	}
	if _, enabled, err := cfg.FindProvider("my-glm"); err != nil || !enabled {
		t.Errorf("custom lookup failed: enabled=%v err=%v", enabled, err)
	}
	if _, _, err := cfg.FindProvider("nope"); err == nil {
		t.Error("expected error for unknown provider")
	}
}

func TestAccountProfilePersistence(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	cfg, err := LoadOrCreateConfig(configPath)
	if err != nil {
		t.Fatalf("failed to create config: %v", err)
	}

	volc := cfg.Providers["volcengine"]
	volc.Enabled = true
	volc.Accounts["phone2"] = Account{Source: SourceArkcli, Plan: "coding", Profile: "p2"}
	cfg.Providers["volcengine"] = volc
	if err := SaveConfig(cfg, configPath); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	loaded, err := LoadOrCreateConfig(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	got := loaded.Providers["volcengine"].Accounts["phone2"]
	if got.Source != SourceArkcli || got.Plan != "coding" || got.Profile != "p2" {
		t.Errorf("account roundtrip mismatch: %+v", got)
	}
}

func TestLanguageFieldPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	cfg, err := LoadOrCreateConfig(configPath)
	if err != nil {
		t.Fatalf("failed to create config: %v", err)
	}

	cfg.Language = "zh"
	if err := SaveConfig(cfg, configPath); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	loaded, err := LoadOrCreateConfig(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if loaded.Language != "zh" {
		t.Errorf("expected language 'zh', got %q", loaded.Language)
	}
}

func TestLanguageFieldDefaultEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	cfg, err := LoadOrCreateConfig(configPath)
	if err != nil {
		t.Fatalf("failed to create config: %v", err)
	}

	if cfg.Language != "" {
		t.Errorf("expected default language '', got %q", cfg.Language)
	}
}

// A config file whose "version" field is missing must never be silently
// reinterpreted (e.g. as legacy v2) and overwritten — that destroys the
// user's providers and accounts (regression: data loss).
func TestLoadOrCreateConfigMissingVersionDoesNotOverwrite(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	original := `providers:
    opencode:
        enabled: true
        default_account: work
        accounts:
            work:
                source: manual
                key_id: "abc123"
custom:
    my-glm:
        query_type: zai-glm
        base_url: https://api.z.ai
        enabled: true
        accounts:
            main:
                source: manual
`
	if err := os.WriteFile(configPath, []byte(original), 0600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	if _, err := LoadOrCreateConfig(configPath); err == nil {
		t.Fatal("expected an error for a config file without a version field")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to re-read config: %v", err)
	}
	if string(data) != original {
		t.Error("config file must not be modified when its version cannot be determined")
	}
}

// An empty (or whitespace-only) config file is treated like a missing
// version: report it, never overwrite it with defaults behind the user's
// back.
func TestLoadOrCreateConfigEmptyFileNotOverwritten(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	if err := os.WriteFile(configPath, []byte("\n"), 0600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	if _, err := LoadOrCreateConfig(configPath); err == nil {
		t.Fatal("expected an error for an empty config file")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to re-read config: %v", err)
	}
	if string(data) != "\n" {
		t.Errorf("empty config file must not be overwritten, got %q", string(data))
	}
}

// Loading a valid v3 config is a read operation: the file must be left
// byte-for-byte untouched (a forced rewrite makes read-only configs fail).
func TestLoadOrCreateConfigV3DoesNotRewriteFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	original := `# hand-tuned config
version: "3"
language: zh
max_concurrent_requests: 7
providers:
    opencode:
        enabled: true
        accounts:
            work:
                source: manual
                key_id: "abc123"
`
	if err := os.WriteFile(configPath, []byte(original), 0600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := LoadOrCreateConfig(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	if cfg.Language != "zh" || cfg.MaxConcurrentRequests != 7 {
		t.Fatalf("unexpected loaded config: %+v", cfg)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to re-read config: %v", err)
	}
	if string(data) != original {
		t.Error("a valid v3 config file must not be rewritten on load")
	}
}

// An explicitly unsupported version must fail without touching the file.
func TestLoadOrCreateConfigUnsupportedVersionKeepsFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	original := "version: \"1\"\n"
	if err := os.WriteFile(configPath, []byte(original), 0600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	if _, err := LoadOrCreateConfig(configPath); err == nil {
		t.Fatal("expected an error for unsupported version")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to re-read config: %v", err)
	}
	if string(data) != original {
		t.Error("config file must not be modified for an unsupported version")
	}
}

// The default account chosen during v2 migration must be deterministic
// (previously it depended on Go's randomized map iteration order).
func TestMigrateV2DefaultAccountDeterministic(t *testing.T) {
	v2 := `version: "2"
accounts:
    alpha:
        name: alpha
        key_id: "aaa111"
    beta:
        name: beta
        key_id: "bbb222"
    gamma:
        name: gamma
        key_id: "ccc333"
`
	for i := 0; i < 50; i++ {
		configPath := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(configPath, []byte(v2), 0600); err != nil {
			t.Fatalf("failed to write config file: %v", err)
		}
		cfg, err := LoadOrCreateConfig(configPath)
		if err != nil {
			t.Fatalf("iteration %d: failed to load config: %v", i, err)
		}
		if got := cfg.Providers["opencode"].DefaultAccount; got != "alpha" {
			t.Fatalf("iteration %d: expected deterministic default account 'alpha', got %q", i, got)
		}
	}
}

// keyIDOf cuts the key id on rune boundaries (it is displayed to users and
// compared against auth.ExtractKeyID, which must agree).
func TestKeyIDOfRuneSafe(t *testing.T) {
	tests := []struct {
		key  string
		want string
	}{
		{"sk-1234567890", "567890"},
		{"short", "short"},
		{"密钥ab密钥cd", "ab密钥cd"},
	}
	for _, tt := range tests {
		if got := keyIDOf(tt.key); got != tt.want {
			t.Errorf("keyIDOf(%q) = %q, want %q", tt.key, got, tt.want)
		}
	}
}

func TestAllAccountsOrder(t *testing.T) {
	cfg := getDefaultConfig()
	cfg.Providers["opencode"].Accounts["b"] = Account{Source: SourceManual}
	cfg.Providers["opencode"].Accounts["a"] = Account{Source: SourceManual}
	cfg.Custom["z-provider"] = CustomProvider{Accounts: map[string]Account{"m": {Source: SourceManual}}}
	cfg.Providers["volcengine"].Accounts["agent-plan"] = Account{Source: SourceLocal, Plan: "agent"}

	got := cfg.AllAccounts()
	var ids []string
	for _, pa := range got {
		ids = append(ids, pa.ProviderID+"/"+pa.Account)
	}
	want := []string{"claude/local", "codex/local", "opencode/a", "opencode/b", "volcengine/agent-plan", "volcengine/coding-plan", "z-provider/m"}
	if len(ids) != len(want) {
		t.Fatalf("expected %d accounts, got %d: %v", len(want), len(ids), ids)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Errorf("order mismatch at %d: got %s want %s", i, ids[i], want[i])
		}
	}
}
