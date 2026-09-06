package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/emmmdty/token-usage/internal/fsutil"
	"github.com/emmmdty/token-usage/internal/i18n"
	"go.yaml.in/yaml/v3"
)

const CurrentVersion = "3"

// Source describes how an account's credentials are resolved.
//   - "manual": the key lives in the credential store (keyring/encrypted file)
//   - "local":  the key is read from the tool's own local config at query time
//     (Claude credentials.json, Codex auth.json, opencode.json, ...)
//   - "arkcli": the credential is the official ark CLI's own login state,
//     selected by Account.Profile (volcengine multi-account)
const (
	SourceManual = "manual"
	SourceLocal  = "local"
	SourceArkcli = "arkcli"
)

type Config struct {
	Version         string                    `yaml:"version"`
	Language        string                    `yaml:"language,omitempty"`
	Providers       map[string]PresetProvider `yaml:"providers"`
	Custom          map[string]CustomProvider `yaml:"custom,omitempty"`
	ColorThresholds struct {
		Warning int `yaml:"warning"`
		Danger  int `yaml:"danger"`
	} `yaml:"color_thresholds"`
	MaxConcurrentRequests int   `yaml:"max_concurrent_requests"`
	UseMasterPassword     *bool `yaml:"use_master_password,omitempty"`
}

// PresetProvider is a built-in provider (opencode, claude, codex,
// volcengine). Optional path fields default to well-known locations when
// empty.
type PresetProvider struct {
	Enabled        bool               `yaml:"enabled"`
	Endpoint       string             `yaml:"endpoint,omitempty"`        // API endpoint override
	CredsPath      string             `yaml:"creds_path,omitempty"`      // claude
	AuthPath       string             `yaml:"auth_path,omitempty"`       // codex
	OpencodeJSON   string             `yaml:"opencode_json,omitempty"`   // volcengine
	DefaultAccount string             `yaml:"default_account,omitempty"` // "current" marker
	Accounts       map[string]Account `yaml:"accounts"`
}

// CustomProvider is a user-defined provider backed by the query registry.
type CustomProvider struct {
	QueryType      string             `yaml:"query_type"` // registry key, e.g. zai-glm
	BaseURL        string             `yaml:"base_url"`
	DisplayName    string             `yaml:"display_name,omitempty"`
	Enabled        bool               `yaml:"enabled"`
	DefaultAccount string             `yaml:"default_account,omitempty"`
	Accounts       map[string]Account `yaml:"accounts"`
}

type Account struct {
	Source  string `yaml:"source"`
	KeyID   string `yaml:"key_id,omitempty"`
	Plan    string `yaml:"plan,omitempty"`    // volcengine: coding | agent
	Profile string `yaml:"profile,omitempty"` // volcengine: arkcli profile (multi-account)
	// OpencodeProvider pins which provider.<id> entry of opencode.json a
	// volcengine "local" account reads its key from (multi-account).
	OpencodeProvider string    `yaml:"opencode_provider,omitempty"`
	CreatedAt        time.Time `yaml:"created_at"`
	LastVerified     time.Time `yaml:"last_verified,omitempty"`
}

// CredentialPath returns the local credential file for "local" source
// accounts of the given preset provider.
func (p *PresetProvider) CredentialPath(providerID, home string) string {
	switch providerID {
	case "claude":
		if p.CredsPath != "" {
			return p.CredsPath
		}
		return filepath.Join(home, ".claude", ".credentials.json")
	case "codex":
		if p.AuthPath != "" {
			return p.AuthPath
		}
		return filepath.Join(home, ".codex", "auth.json")
	case "volcengine":
		if p.OpencodeJSON != "" {
			return p.OpencodeJSON
		}
		return filepath.Join(home, ".config", "opencode", "opencode.json")
	}
	return ""
}

// CredentialPath returns the local credential file for "local" source
// accounts of a custom provider. Custom providers always use stored keys,
// so this returns "".
func (c *CustomProvider) CredentialPath() string {
	return ""
}

// ProviderAccount is a flattened (providerID, accountName, account) triple
// used when iterating accounts across all providers.
type ProviderAccount struct {
	ProviderID string
	Account    string
	Data       Account
}

// AllAccounts iterates preset and custom providers' accounts in a stable
// order (provider name, then account name).
func (c *Config) AllAccounts() []ProviderAccount {
	var out []ProviderAccount

	presetIDs := make([]string, 0, len(c.Providers))
	for id := range c.Providers {
		presetIDs = append(presetIDs, id)
	}
	sortStrings(presetIDs)
	for _, id := range presetIDs {
		p := c.Providers[id]
		names := make([]string, 0, len(p.Accounts))
		for name := range p.Accounts {
			names = append(names, name)
		}
		sortStrings(names)
		for _, name := range names {
			out = append(out, ProviderAccount{ProviderID: id, Account: name, Data: p.Accounts[name]})
		}
	}

	customIDs := make([]string, 0, len(c.Custom))
	for id := range c.Custom {
		customIDs = append(customIDs, id)
	}
	sortStrings(customIDs)
	for _, id := range customIDs {
		p := c.Custom[id]
		names := make([]string, 0, len(p.Accounts))
		for name := range p.Accounts {
			names = append(names, name)
		}
		sortStrings(names)
		for _, name := range names {
			out = append(out, ProviderAccount{ProviderID: id, Account: name, Data: p.Accounts[name]})
		}
	}
	return out
}

// FindProvider locates a provider (preset first, then custom) and reports
// whether it is enabled.
func (c *Config) FindProvider(id string) (accounts map[string]Account, enabled bool, err error) {
	if p, ok := c.Providers[id]; ok {
		return p.Accounts, p.Enabled, nil
	}
	if p, ok := c.Custom[id]; ok {
		return p.Accounts, p.Enabled, nil
	}
	return nil, false, fmt.Errorf("%s", i18n.T("error.config.not_found", id))
}

func getDefaultConfig() *Config {
	cfg := &Config{
		Version:               CurrentVersion,
		Providers:             make(map[string]PresetProvider),
		Custom:                make(map[string]CustomProvider),
		MaxConcurrentRequests: 5,
	}
	cfg.ColorThresholds.Warning = 50
	cfg.ColorThresholds.Danger = 80

	// Presets ship enabled with one implicit "local" account each; the local
	// account is inert when its credential file does not exist.
	cfg.Providers["opencode"] = PresetProvider{
		Enabled:  true,
		Accounts: map[string]Account{},
	}
	cfg.Providers["claude"] = PresetProvider{
		Enabled:  true,
		Accounts: map[string]Account{"local": {Source: SourceLocal}},
	}
	cfg.Providers["codex"] = PresetProvider{
		Enabled:  true,
		Accounts: map[string]Account{"local": {Source: SourceLocal}},
	}
	cfg.Providers["volcengine"] = PresetProvider{
		Enabled:  false,
		Accounts: map[string]Account{"coding-plan": {Source: SourceLocal, Plan: "coding"}},
	}
	return cfg
}

func LoadOrCreateConfig(path string) (*Config, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		cfg := getDefaultConfig()
		if err := saveConfig(cfg, path); err != nil {
			return nil, err
		}
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	probe := struct {
		Version string `yaml:"version"`
	}{}
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return nil, err
	}

	var cfg *Config
	migrated := false
	switch probe.Version {
	case CurrentVersion:
		cfg = &Config{}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, err
		}
	case "2":
		var legacy legacyV2Config
		if err := yaml.Unmarshal(data, &legacy); err != nil {
			return nil, err
		}
		cfg = migrateV2ToV3(&legacy)
		migrated = true
	case "":
		// A missing version means the file's format cannot be identified.
		// Guessing would silently reinterpret (and then overwrite) valid
		// data — for example a hand-edited v3 config that lost its version
		// line — so fail instead of risking data loss.
		return nil, fmt.Errorf("%s", i18n.T("error.config.missing_version"))
	default:
		return nil, fmt.Errorf("%s", i18n.T("error.config.unsupported_version", probe.Version, CurrentVersion))
	}

	// Only persist when the on-disk format actually changed (v2 migration).
	// Rewriting on every load would turn a read into a write and fail for
	// read-only config files.
	if migrated {
		if err := saveConfig(cfg, path); err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

func saveConfig(cfg *Config, path string) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	return fsutil.WriteFileAtomic(path, data, 0600)
}

func SaveConfig(cfg *Config, path string) error {
	return saveConfig(cfg, path)
}

// legacyV2Config mirrors the v2 on-disk schema: a flat global accounts map
// plus per-provider toggles with at most one inline credential each.
type legacyV2Config struct {
	Version         string                    `yaml:"version"`
	Accounts        map[string]LegacyAccount  `yaml:"accounts"`
	Providers       map[string]LegacyProvider `yaml:"providers"`
	ColorThresholds struct {
		Warning int `yaml:"warning"`
		Danger  int `yaml:"danger"`
	} `yaml:"color_thresholds"`
	MaxConcurrentRequests int   `yaml:"max_concurrent_requests"`
	UseMasterPassword     *bool `yaml:"use_master_password,omitempty"`
}

type LegacyAccount struct {
	Name         string    `yaml:"name"`
	KeyID        string    `yaml:"key_id"`
	CreatedAt    time.Time `yaml:"created_at"`
	LastVerified time.Time `yaml:"last_verified"`
}

type LegacyProvider struct {
	Enabled   bool   `yaml:"enabled"`
	APIKey    string `yaml:"api_key,omitempty"`
	CredsPath string `yaml:"creds_path,omitempty"`
	AuthPath  string `yaml:"auth_path,omitempty"`
	Endpoint  string `yaml:"endpoint,omitempty"`
}

// migrateV2ToV3 converts the flat global accounts table into per-provider
// accounts. All v2 accounts were OpenCode Go keys, so they land under
// providers.opencode. Credential-store key renames ("name" ->
// "opencode/name") are handled by the caller via ListAccountKeys-style
// reconciliation, so this function only rewrites the config file.
func migrateV2ToV3(old *legacyV2Config) *Config {
	cfg := getDefaultConfig()

	if old.ColorThresholds.Warning != 0 {
		cfg.ColorThresholds.Warning = old.ColorThresholds.Warning
	}
	if old.ColorThresholds.Danger != 0 {
		cfg.ColorThresholds.Danger = old.ColorThresholds.Danger
	}
	if old.MaxConcurrentRequests != 0 {
		cfg.MaxConcurrentRequests = old.MaxConcurrentRequests
	}
	cfg.UseMasterPassword = old.UseMasterPassword

	if old.Providers != nil {
		for k, v := range old.Providers {
			if p, ok := cfg.Providers[k]; ok {
				p.Enabled = v.Enabled
				p.CredsPath = v.CredsPath
				p.AuthPath = v.AuthPath
				p.OpencodeJSON = v.Endpoint
				if _, hasLocal := p.Accounts["local"]; !hasLocal && (k == "claude" || k == "codex") {
					p.Accounts["local"] = Account{Source: SourceLocal}
				}
				cfg.Providers[k] = p
			}
		}
	}

	// Flat v2 accounts become opencode accounts.
	if len(old.Accounts) > 0 {
		oc := cfg.Providers["opencode"]
		for name, acc := range old.Accounts {
			oc.Accounts[name] = Account{
				Source:       SourceManual,
				KeyID:        acc.KeyID,
				CreatedAt:    acc.CreatedAt,
				LastVerified: acc.LastVerified,
			}
		}
		if oc.DefaultAccount == "" {
			oc.DefaultAccount = defaultAccountName(oc.Accounts)
		}
		cfg.Providers["opencode"] = oc
	}

	// v2 volcengine had at most one inline API key.
	if volc, ok := old.Providers["volcengine"]; ok && volc.Enabled && volc.APIKey != "" {
		v := cfg.Providers["volcengine"]
		v.Enabled = true
		v.Accounts["coding-plan"] = Account{
			Source:    SourceManual,
			KeyID:     keyIDOf(volc.APIKey),
			Plan:      "coding",
			CreatedAt: time.Now(),
		}
		cfg.Providers["volcengine"] = v
	}

	return cfg
}

func defaultAccountName(accounts map[string]Account) string {
	// Map iteration order is randomized in Go; sort so migrations are
	// deterministic (first account alphabetically).
	names := make([]string, 0, len(accounts))
	for name := range accounts {
		names = append(names, name)
	}
	sortStrings(names)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

func sortStrings(s []string) { sort.Strings(s) }

// keyIDOf returns the last 6 characters (rune-safe) of the key. It must
// agree with auth.ExtractKeyID, which is used for matching.
func keyIDOf(key string) string {
	runes := []rune(key)
	if len(runes) > 6 {
		return string(runes[len(runes)-6:])
	}
	return key
}

// DefaultTestConfig exposes the default config for cmd-layer tests.
func DefaultTestConfig() *Config { return getDefaultConfig() }
