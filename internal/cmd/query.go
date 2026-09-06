package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
	"unicode"

	"github.com/emmmdty/token-usage/internal/auth"
	"github.com/emmmdty/token-usage/internal/config"
	"github.com/emmmdty/token-usage/internal/i18n"
	"github.com/emmmdty/token-usage/internal/provider"
	"github.com/mattn/go-runewidth"
)

// queryTarget is one concrete (provider, account) pair resolved to a
// queryable unit. It carries display metadata plus a closure that performs
// the actual quota fetch.
type queryTarget struct {
	ProviderID   string
	Account      string
	Display      string // display name, e.g. "Volcano Engine (Coding Plan)"
	ProviderCode string // short column code, e.g. "VE-C"
	AccountLabel string // plain account name for the account column
	Plan         string // volcengine: coding | agent
	Name         string // row name: display, plus " · account" when ambiguous
	IsCurrent    bool
	query        func() (*provider.Usage, error)
}

// displayName maps a provider id + plan to its human-readable name.
func displayName(providerID, plan string, custom *config.CustomProvider) string {
	if custom != nil {
		if custom.DisplayName != "" {
			return custom.DisplayName
		}
		return providerID
	}
	switch providerID {
	case "opencode":
		return "OpenCode Go"
	case "claude":
		return "Claude"
	case "codex":
		return "Codex"
	case "volcengine":
		if plan == provider.PlanAgent {
			return "Volcano Engine (Agent Plan)"
		}
		return "Volcano Engine (Coding Plan)"
	}
	return providerID
}

// resolveStoredKey fetches a manual credential under "provider/account",
// falling back to the legacy flat key name and re-keying on success.
func resolveStoredKey(providerID, account string) (string, error) {
	composite := providerID + "/" + account
	key, err := auth.GetAPIKey("token-usage", composite)
	if err == nil {
		return key, nil
	}
	// Legacy migration: v2 stored keys under the bare account name.
	legacy, legacyErr := auth.GetAPIKey("token-usage", account)
	if legacyErr == nil {
		if storeErr := auth.StoreAPIKey("token-usage", composite, legacy); storeErr == nil {
			_ = auth.DeleteAPIKey("token-usage", account)
		}
		return legacy, nil
	}
	return "", err
}

// resolveLocalKey builds a query closure for "local" source accounts, where
// credentials live in each tool's own config files.
func resolveLocalKey(cfg *config.Config, providerID, account string) (func() (*provider.Usage, error), error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to resolve home directory: %w", err)
	}
	preset, ok := cfg.Providers[providerID]
	if !ok {
		return nil, fmt.Errorf("provider '%s' does not support local credentials", providerID)
	}
	credPath := preset.CredentialPath(providerID, home)
	if credPath == "" {
		return nil, fmt.Errorf("no credential path for provider '%s'", providerID)
	}
	if _, err := os.Stat(credPath); err != nil && !(providerID == "volcengine" && preset.Accounts[account].Profile != "") {
		return nil, fmt.Errorf("local credentials not found at %s", credPath)
	}

	switch providerID {
	case "claude":
		p := provider.NewClaudeProviderWithEndpoint(credPath, preset.Endpoint)
		return func() (*provider.Usage, error) { return p.GetUsage() }, nil
	case "codex":
		p := provider.NewCodexProviderWithEndpoint(credPath, preset.Endpoint)
		return func() (*provider.Usage, error) { return p.GetUsage() }, nil
	case "volcengine":
		acc := preset.Accounts[account]
		plan := acc.Plan
		if plan == "" {
			plan = provider.PlanCoding
		}
		// The opencode.json key powers the probe fallback; a profile-backed
		// account works without it as long as arkcli carries the login.
		apiKey, keyErr := volcengineKeyForEntry(credPath, acc.OpencodeProvider)
		if keyErr != nil && acc.Profile == "" {
			return nil, keyErr
		}
		p := provider.NewVolcengineProvider(apiKey, plan, acc.Profile, acc.ArkcliHome)
		return func() (*provider.Usage, error) { return p.GetUsage() }, nil
	}
	return nil, fmt.Errorf("provider '%s' does not support local credentials", providerID)
}

// volcengineOpencodeEntry is one Volcano provider entry in opencode.json.
type volcengineOpencodeEntry struct {
	ID  string // opencode.json provider.<id>
	Key string // provider.<id>.options.apiKey
}

// volcengineOpencodeEntries parses every Volcano provider entry that
// carries an API key. opencode.json may hold several accounts at once
// (e.g. one per phone number); order follows the JSON document.
func volcengineOpencodeEntries(path string) ([]volcengineOpencodeEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("opencode.json not readable (%s): %w", path, err)
	}
	var doc struct {
		Provider map[string]struct {
			Options struct {
				APIKey string `json:"apiKey"`
			} `json:"options"`
		} `json:"provider"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", path, err)
	}
	var entries []volcengineOpencodeEntry
	for id, p := range doc.Provider {
		if p.Options.APIKey != "" {
			entries = append(entries, volcengineOpencodeEntry{ID: id, Key: p.Options.APIKey})
		}
	}
	// Map order is random in Go; sort by id so detection is deterministic.
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	return entries, nil
}

// volcengineKeyForEntry returns the API key of one opencode.json provider
// entry, falling back to the legacy candidates ("volcengine-coding-plan",
// "coding-plan", first key) when no explicit binding is set.
func volcengineKeyForEntry(path, ref string) (string, error) {
	entries, err := volcengineOpencodeEntries(path)
	if err != nil {
		return "", err
	}
	if ref != "" {
		for _, e := range entries {
			if e.ID == ref {
				return e.Key, nil
			}
		}
		return "", fmt.Errorf("no provider '%s' in %s", ref, path)
	}
	for _, candidate := range []string{"volcengine-coding-plan", "coding-plan"} {
		for _, e := range entries {
			if e.ID == candidate {
				return e.Key, nil
			}
		}
	}
	if len(entries) > 0 {
		return entries[0].Key, nil
	}
	return "", fmt.Errorf("no API key found in %s (provider.<id>.options.apiKey)", path)
}

// volcengineKeyFromOpencodeJSON extracts the coding-plan API key from the
// user's opencode config so the volcano provider works with zero setup.
func volcengineKeyFromOpencodeJSON(path string) (string, error) {
	return volcengineKeyForEntry(path, "")
}

// resolveCustomKey returns the query closure for a custom provider account.
func resolveCustomKey(custom config.CustomProvider, providerID, account string) (func() (*provider.Usage, error), error) {
	q, ok := provider.LookupKeyQuery(custom.QueryType)
	if !ok {
		return nil, fmt.Errorf("unknown query type '%s'", custom.QueryType)
	}
	key, err := resolveStoredKey(providerID, account)
	if err != nil {
		return nil, fmt.Errorf("credential not found: %w", err)
	}
	return func() (*provider.Usage, error) { return q(key, custom.BaseURL) }, nil
}

// buildTargets resolves the full (provider, account) matrix into query
// targets, applying optional provider/account filters. Targets for disabled
// providers or missing credentials are skipped (reported via returned notes).
func buildTargets(cfg *config.Config, providerFilter, accountFilter string) ([]queryTarget, []string, error) {
	var targets []queryTarget
	var notes []string

	// Count accounts per provider so single-account providers can omit the
	// account suffix in display names.
	accountCount := map[string]int{}
	for _, pa := range cfg.AllAccounts() {
		accountCount[pa.ProviderID]++
	}

	for _, pa := range cfg.AllAccounts() {
		if providerFilter != "" && pa.ProviderID != providerFilter {
			continue
		}
		if accountFilter != "" && pa.Account != accountFilter {
			continue
		}

		enabled := false
		var custom *config.CustomProvider
		if c, ok := cfg.Custom[pa.ProviderID]; ok {
			custom = &c
			enabled = c.Enabled
		} else if p, ok := cfg.Providers[pa.ProviderID]; ok {
			enabled = p.Enabled
		}
		if !enabled {
			continue
		}

		display := displayName(pa.ProviderID, pa.Data.Plan, custom)
		name := display
		if accountCount[pa.ProviderID] > 1 {
			name = display + " · " + pa.Account
		}

		mkQuery := func() (func() (*provider.Usage, error), error) {
			switch {
			case custom != nil:
				return resolveCustomKey(*custom, pa.ProviderID, pa.Account)
			case pa.Data.Source == config.SourceArkcli:
				if pa.ProviderID != "volcengine" {
					return nil, fmt.Errorf("source 'arkcli' is only supported for the volcengine provider")
				}
				// Credential-free: arkcli's login state (selected via the
				// account's profile) is the credential.
				acc := cfg.Providers["volcengine"].Accounts[pa.Account]
				plan, profile := volcenginePlanProfile(cfg, pa.Account)
				p := provider.NewVolcengineProvider("", plan, profile, acc.ArkcliHome)
				return func() (*provider.Usage, error) { return p.GetUsage() }, nil
			case pa.Data.Source == config.SourceLocal:
				return resolveLocalKey(cfg, pa.ProviderID, pa.Account)
			default:
				key, err := resolveStoredKey(pa.ProviderID, pa.Account)
				if err != nil {
					return nil, err
				}
				if pa.ProviderID == "volcengine" {
					acc := cfg.Providers["volcengine"].Accounts[pa.Account]
					plan, profile := volcenginePlanProfile(cfg, pa.Account)
					p := provider.NewVolcengineProvider(key, plan, profile, acc.ArkcliHome)
					return func() (*provider.Usage, error) { return p.GetUsage() }, nil
				}
				if q, ok := provider.LookupKeyQuery(pa.ProviderID); ok {
					return func() (*provider.Usage, error) { return q(key, "") }, nil
				}
				return nil, fmt.Errorf("no query implementation for provider '%s'", pa.ProviderID)
			}
		}

		query, err := mkQuery()
		if err != nil {
			notes = append(notes, fmt.Sprintf("%s/%s: %v", pa.ProviderID, pa.Account, err))
			continue
		}

		targets = append(targets, queryTarget{
			ProviderID:   pa.ProviderID,
			Account:      pa.Account,
			Display:      display,
			AccountLabel: pa.Account,
			Plan:         pa.Data.Plan,
			Name:         name,
			IsCurrent:    isCurrentAccount(cfg, pa.ProviderID, pa.Account),
			query:        query,
		})
	}
	assignProviderCodes(targets, cfg)
	return targets, notes, nil
}

// providerCode renders the short table code for providers whose display
// name is too long for the table ("VE-C" for the Volcano coding plan).
// Short names (Claude, Codex, OpenCode Go, short custom names) return ""
// and are shown as-is; long custom names get an initials-derived code.
// Collisions are resolved with a numeric suffix.
func providerCode(providerID, plan string, custom *config.CustomProvider, taken map[string]int) string {
	var code string
	switch providerID {
	case "volcengine":
		if plan == provider.PlanAgent {
			code = "VE-A"
		} else {
			code = "VE-C"
		}
	case "claude", "codex", "opencode":
		return ""
	default:
		name := providerID
		if custom != nil && custom.DisplayName != "" {
			name = custom.DisplayName
		}
		if runewidth.StringWidth(name) <= 10 {
			return ""
		}
		code = abbreviateProviderName(name)
	}
	taken[code]++
	if n := taken[code]; n > 1 {
		code = fmt.Sprintf("%s%d", code, n)
	}
	return code
}

// abbreviateProviderName derives an uppercase code (max 4 chars) from a
// display name: word initials when there are several words, otherwise the
// leading letters ("Z.ai GLM" -> "ZG", "deepseek" -> "DE").
func abbreviateProviderName(name string) string {
	var initials []rune
	startWord := true
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			startWord = true
			continue
		}
		if startWord {
			initials = append(initials, unicode.ToUpper(r))
			startWord = false
		}
	}
	if len(initials) < 2 {
		letters := []rune{}
		for _, r := range name {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				letters = append(letters, unicode.ToUpper(r))
			}
		}
		initials = letters
	}
	if len(initials) > 4 {
		initials = initials[:4]
	}
	return string(initials)
}

// assignProviderCodes fills the ProviderCode of every target in place.
func assignProviderCodes(targets []queryTarget, cfg *config.Config) {
	taken := map[string]int{}
	for i := range targets {
		var custom *config.CustomProvider
		if c, ok := cfg.Custom[targets[i].ProviderID]; ok {
			custom = &c
		}
		targets[i].ProviderCode = providerCode(targets[i].ProviderID, targets[i].Plan, custom, taken)
	}
}

// volcenginePlanProfile returns the plan and arkcli profile configured for a
// volcengine account, defaulting the plan to coding.
func volcenginePlanProfile(cfg *config.Config, account string) (plan, profile string) {
	plan = provider.PlanCoding
	if p, ok := cfg.Providers["volcengine"]; ok {
		if acc, ok := p.Accounts[account]; ok {
			if acc.Plan != "" {
				plan = acc.Plan
			}
			profile = acc.Profile
		}
	}
	return plan, profile
}

// isCurrentAccount reports whether (provider, account) is marked current:
// either via the per-provider default_account, or — for opencode — via the
// key currently configured in opencode's own auth.json.
func isCurrentAccount(cfg *config.Config, providerID, account string) bool {
	if p, ok := cfg.Providers[providerID]; ok && p.DefaultAccount == account {
		return true
	}
	if c, ok := cfg.Custom[providerID]; ok && c.DefaultAccount == account {
		return true
	}
	return false
}

// runQueryTargets executes all targets concurrently and returns per-target
// results in stable (provider, account) order.
type queryResult struct {
	Target queryTarget
	Usage  *provider.Usage
	Err    error
}

// queryOverallTimeout bounds a whole quota run: one wedged provider must
// not hold the entire report hostage. Variable for test injection.
var queryOverallTimeout = 60 * time.Second

func runQueryTargets(cfg *config.Config, targets []queryTarget) []queryResult {
	if len(targets) == 0 {
		return nil
	}

	maxConcurrent := cfg.MaxConcurrentRequests
	if maxConcurrent <= 0 {
		maxConcurrent = 5
	}
	sem := make(chan struct{}, maxConcurrent)

	// Each worker reports through its own buffered slot so a goroutine that
	// finishes after the deadline never blocks or races with the results
	// slice (its late message is simply absorbed by the buffer).
	type resultMsg struct {
		idx int
		res queryResult
	}
	msgs := make(chan resultMsg, len(targets))

	results := make([]queryResult, len(targets))
	completed := make([]bool, len(targets))
	var wg sync.WaitGroup
	for i := range targets {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			usage, err := targets[i].query()
			msgs <- resultMsg{i, queryResult{Target: targets[i], Usage: usage, Err: err}}
		}(i)
	}

	deadline := time.After(queryOverallTimeout)
	for done := 0; done < len(targets); {
		select {
		case msg := <-msgs:
			results[msg.idx] = msg.res
			completed[msg.idx] = true
			done++
		case <-deadline:
			for i := range results {
				if !completed[i] {
					results[i] = queryResult{
						Target: targets[i],
						Err:    fmt.Errorf("%s", i18n.T("error.query.timeout", queryOverallTimeout)),
					}
				}
			}
			return results
		}
	}
	return results
}

// defaultAccountFor returns the default account of a provider, if set.
func defaultAccountFor(cfg *config.Config, providerID string) string {
	if p, ok := cfg.Providers[providerID]; ok {
		return p.DefaultAccount
	}
	if c, ok := cfg.Custom[providerID]; ok {
		return c.DefaultAccount
	}
	return ""
}

// touchLastVerified stamps last_verified for a successfully queried account.
func touchLastVerified(cfg *config.Config, providerID, account string, path string) {
	stamp := func(acc config.Account) config.Account {
		acc.LastVerified = time.Now()
		return acc
	}
	if p, ok := cfg.Providers[providerID]; ok {
		if acc, ok := p.Accounts[account]; ok {
			p.Accounts[account] = stamp(acc)
			cfg.Providers[providerID] = p
		}
	} else if c, ok := cfg.Custom[providerID]; ok {
		if acc, ok := c.Accounts[account]; ok {
			c.Accounts[account] = stamp(acc)
			cfg.Custom[providerID] = c
		}
	}
	if err := config.SaveConfig(cfg, path); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", i18n.T("warning.config.save_failed", err))
	}
}

// opencodeAuthPath is where opencode stores the active provider keys.
func opencodeAuthPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "opencode", "auth.json")
}

func timeNow() time.Time { return time.Now() }

func filepathDir(path string) string { return filepath.Dir(path) }

// validateProviderKey does a live check of a key before persisting it.
func validateProviderKey(providerType, plan, apiKey string) error {
	switch providerType {
	case "volcengine":
		p := provider.NewVolcengineProvider(apiKey, plan, "", "")
		_, err := p.GetUsage()
		return err
	case "opencode":
		q, _ := provider.LookupKeyQuery("opencode")
		_, err := q(apiKey, "")
		return err
	case "claude":
		p := provider.NewClaudeProviderWithToken(apiKey, "")
		_, err := p.GetUsage()
		return err
	case "codex":
		p := provider.NewCodexProviderWithToken(apiKey, "")
		_, err := p.GetUsage()
		return err
	}
	return fmt.Errorf("no validation for provider '%s'", providerType)
}

// formatRelativeTime renders a timestamp as a human-friendly relative time.
func formatRelativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		mins := int(d.Minutes())
		if mins == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d mins ago", mins)
	case d < 24*time.Hour:
		hours := int(d.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	case d < 30*24*time.Hour:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	default:
		return t.Format("2006-01-02")
	}
}
