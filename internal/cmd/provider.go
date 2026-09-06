package cmd

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"

	"errors"
	"github.com/emmmdty/token-usage/internal/auth"
	"github.com/emmmdty/token-usage/internal/config"
	"github.com/emmmdty/token-usage/internal/i18n"
	"github.com/emmmdty/token-usage/internal/provider"
	"github.com/spf13/cobra"
)

// presetMenu describes the built-in providers offered by `provider add`.
type presetMenu struct {
	id    string
	label string
}

var presetMenuItems = []presetMenu{
	{"opencode", "opencode-go      OpenCode Go"},
	{"claude", "claude           Claude (uses local Claude Code login by default)"},
	{"codex", "codex            Codex (uses local Codex login by default)"},
	{"volcengine", "volcengine       Volcano Engine (Coding Plan)"},
	{"custom", "custom           Custom provider (name + base URL + API key)"},
}

var providerCmd = &cobra.Command{
	Use:     "provider",
	Aliases: []string{"pr"},
	Short:   "Manage providers",
	Long: `Manage providers and their account lists.

Built-in presets (opencode, claude, codex, volcengine) ship enabled with a
local account where applicable. Custom providers wrap one of the built-in
query implementations (zai-glm, kimi, minimax, deepseek, openai-compatible)
with your own base URL and API key, and are only saved after a live quota
query succeeds.

Run 'token-usage provider add' to register a new provider.`,
}

var providerListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List configured providers",
	Long: `List configured providers with account counts and status.

Shows, for every preset and custom provider: display name, enabled state,
number of accounts, the current account marker (->), and per-account
details (credential source, masked key id, plan).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfgPath, err := getConfigPath()
		if err != nil {
			return err
		}
		cfg, err := config.LoadOrCreateConfig(cfgPath)
		if err != nil {
			return err
		}
		configureAuthFromConfig(cfg)

		return writeOutput(renderProviderList(cfg))
	},
}

func renderProviderList(cfg *config.Config) string {
	var b strings.Builder
	b.WriteString("\n  " + i18n.T("output.provider.list.header") + "\n\n")

	ids := make([]string, 0, len(cfg.Providers)+len(cfg.Custom))
	for id := range cfg.Providers {
		ids = append(ids, id)
	}
	for id := range cfg.Custom {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		var (
			display  string
			enabled  bool
			defAcc   string
			accounts map[string]config.Account
			queryTyp string
		)
		if p, ok := cfg.Providers[id]; ok {
			enabled = p.Enabled
			defAcc = p.DefaultAccount
			accounts = p.Accounts
		} else {
			c := cfg.Custom[id]
			enabled = c.Enabled
			defAcc = c.DefaultAccount
			accounts = c.Accounts
			queryTyp = " (" + c.QueryType + ")"
		}
		display = displayName(id, planOf(accounts, defAcc), customPtr(cfg, id))

		state := i18n.T("output.provider.list.disabled")
		if enabled {
			state = i18n.T("output.provider.list.enabled")
		}
		marker := "  "
		if defAcc != "" {
			marker = "-> "
		}
		fmt.Fprintf(&b, "  %s%-14s %-22s %-9s %s", marker, id, display+queryTyp, state, i18n.T("output.provider.list.accounts", len(accounts)))
		if defAcc != "" {
			fmt.Fprintf(&b, "  "+i18n.T("output.provider.list.default"), defAcc)
		}
		b.WriteString("\n")
		for _, name := range sortedAccountNames(accounts) {
			acc := accounts[name]
			src := acc.Source
			if src == "" {
				src = config.SourceManual
			}
			detail := src
			if acc.KeyID != "" {
				detail += "  key: sk-..." + acc.KeyID
			}
			if acc.Plan != "" {
				detail += "  plan: " + acc.Plan
			}
			fmt.Fprintf(&b, "      %-16s %s\n", name, detail)
		}
	}
	b.WriteString("\n  " + i18n.T("output.provider.list.footer") + "\n\n")
	return b.String()
}

func planOf(accounts map[string]config.Account, def string) string {
	if acc, ok := accounts[def]; ok && acc.Plan != "" {
		return acc.Plan
	}
	for _, acc := range accounts {
		if acc.Plan != "" {
			return acc.Plan
		}
	}
	return ""
}

func customPtr(cfg *config.Config, id string) *config.CustomProvider {
	if c, ok := cfg.Custom[id]; ok {
		return &c
	}
	return nil
}

func sortedAccountNames(accounts map[string]config.Account) []string {
	names := make([]string, 0, len(accounts))
	for name := range accounts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

var (
	addProviderType string
	addAccountName  string
	addQueryType    string
	addBaseURL      string
	addAPIKey       string
	addPlan         string
	addUseLocal     bool
	addProfile      string
	addLocalRef     string
)

var providerAddCmd = &cobra.Command{
	Use:   "add [type]",
	Short: "Add a provider (preset or custom)",
	Long: `Add a provider interactively, or non-interactively with flags.

Presets: opencode, claude, codex, volcengine
Custom:  provider add custom --name <id> --query-type <zai-glm|kimi|minimax|deepseek|openai-compatible>
         --base-url <url> --key <api-key>

Preset providers detect local logins (Claude Code, Codex, opencode.json) and
offer to reuse them without modifying those files. The Volcano Engine preset
reads its API key from ~/.config/opencode/opencode.json automatically.

Custom providers are validated with a live quota query before being saved:
if the query fails (bad key, unreachable URL, no quota endpoint) nothing is
written and the reason is printed.

Examples:
  token-usage provider add                        # interactive menu
  token-usage provider add volcengine --plan coding --use-local
  token-usage provider add custom --name my-glm --query-type zai-glm \
      --base-url https://api.z.ai --key <api-key>`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		providerType := addProviderType
		if len(args) > 0 {
			providerType = args[0]
		}

		cfgPath, err := getConfigPath()
		if err != nil {
			return err
		}
		cfg, err := config.LoadOrCreateConfig(cfgPath)
		if err != nil {
			return err
		}
		configureAuthFromConfig(cfg)

		reader := bufio.NewReader(os.Stdin)
		if providerType == "" {
			opts := make([]string, len(presetMenuItems))
			for i, item := range presetMenuItems {
				opts[i] = item.label
			}
			idx, err := promptSelect(reader, i18n.T("output.provider.add.provider_type"), opts)
			if err != nil {
				return err
			}
			providerType = presetMenuItems[idx].id
		}

		switch providerType {
		case "opencode", "claude", "codex", "volcengine":
			return addPresetProvider(cfg, cfgPath, reader, providerType, addOpts{
				name:     addAccountName,
				apiKey:   addAPIKey,
				useLocal: addUseLocal,
				plan:     addPlan,
				profile:  addProfile,
				localRef: addLocalRef,
			})
		case "custom":
			return addCustomProvider(cfg, cfgPath, reader, addOpts{
				name:      addAccountName,
				apiKey:    addAPIKey,
				queryType: addQueryType,
				baseURL:   addBaseURL,
			})
		default:
			return fmt.Errorf("unknown provider type '%s' (presets: opencode, claude, codex, volcengine; or 'custom')", providerType)
		}
	},
}

// addOpts carries the non-interactive flags shared by `provider add` and
// `account add`. Empty values trigger interactive prompts.
type addOpts struct {
	name      string
	apiKey    string
	useLocal  bool
	plan      string
	queryType string
	baseURL   string
	profile   string // volcengine: arkcli profile (multi-account)
	localRef  string // volcengine: opencode.json provider entry (multi-account)
}

// arkcliProfileOptions renders one picker line per arkcli profile:
// name (display name) [type], with the default profile marked.
func arkcliProfileOptions(profiles []provider.ArkcliProfile) []string {
	opts := make([]string, 0, len(profiles))
	for _, pr := range profiles {
		label := pr.Name
		if pr.DisplayName != "" {
			label += " (" + pr.DisplayName + ")"
		}
		if pr.Type != "" {
			label += " [" + pr.Type + "]"
		}
		if pr.IsDefault {
			label += i18n.T("output.account.switch.current_marker")
		}
		opts = append(opts, label)
	}
	return opts
}

// addPresetProvider walks the interactive flow for a preset provider: reuse
// the locally logged-in account or store a new API key.
func addPresetProvider(cfg *config.Config, cfgPath string, reader *bufio.Reader, providerType string, opts addOpts) error {
	p := cfg.Providers[providerType]

	// Determine the plan for volcengine.
	plan := ""
	if providerType == "volcengine" {
		plan = opts.plan
		if plan == "" {
			opts2 := []string{"coding", "agent"}
			idx, err := promptSelect(reader, i18n.T("output.provider.add.subscription_type"), []string{
				i18n.T("output.provider.add.plan_coding"),
				i18n.T("output.provider.add.plan_agent"),
			})
			if err != nil {
				return err
			}
			plan = opts2[idx]
		}
		if plan != provider.PlanCoding && plan != provider.PlanAgent {
			return fmt.Errorf("plan must be 'coding' or 'agent'")
		}
	}

	// Local detection.
	localDesc := detectLocalAccount(cfg, providerType)
	useLocal := opts.useLocal
	if localDesc != "" && !useLocal && opts.apiKey == "" {
		yes, err := promptYesNo(reader, fmt.Sprintf("  Detected local account (%s). Use it?", localDesc), true)
		if err != nil {
			return err
		}
		useLocal = yes
	}

	// Resolve the arkcli profile for volcengine accounts. Each Volcano
	// account login is one arkcli profile. Interactive binding is only
	// offered when the flow will not take a local account or an API key —
	// key-backed accounts match their profile by key suffix automatically.
	profile := opts.profile
	if providerType == "volcengine" && profile == "" && opts.apiKey == "" && !useLocal && provider.ArkcliAvailable() {
		profiles, err := provider.ArkcliProfiles()
		if err == nil && len(profiles) > 0 {
			yes, err := promptYesNo(reader, "  "+i18n.T("prompt.profile_bind"), true)
			if err != nil {
				return err
			}
			if yes {
				idx, err := promptSelect(reader, i18n.T("prompt.profile_select"), arkcliProfileOptions(profiles))
				if err != nil {
					return err
				}
				profile = profiles[idx].Name
			}
		}
	}

	var accountName string
	var acc config.Account

	switch {
	case useLocal && localDesc != "":
		if providerType == "volcengine" {
			// opencode.json can carry several Volcano accounts; register
			// one local account per entry, pinned to its provider id.
			return addVolcengineLocalAccounts(cfg, cfgPath, p, plan, profile, opts)
		}
		accountName = opts.name
		if accountName == "" {
			accountName = "local"
		}
		acc = config.Account{Source: config.SourceLocal, Plan: plan, CreatedAt: timeNow()}
		ensureProviderEnabled(cfg, providerType, true)
	case providerType == "volcengine" && profile != "":
		// Profile-only account: the arkcli login IS the credential, no API
		// key needed. Validate the profile live so broken bindings never
		// reach the config.
		fmt.Println("  " + i18n.T("output.provider.add.validating_profile"))
		probe := provider.NewVolcengineProvider("", plan, profile)
		if _, err := probe.GetUsage(); err != nil {
			return fmt.Errorf("%s", i18n.T("error.account.key_validation_failed", err))
		}

		accountName = opts.name
		if accountName == "" {
			name, err := promptInput(reader, i18n.T("prompt.account_name"))
			if err != nil {
				return err
			}
			accountName = name
		}
		if strings.ContainsAny(accountName, "/\n\x00:") {
			return errors.New(i18n.T("error.account.name_invalid"))
		}
		if _, exists := p.Accounts[accountName]; exists {
			return fmt.Errorf("%s", i18n.T("error.account.already_exists", accountName, providerType))
		}
		acc = config.Account{Source: config.SourceArkcli, Plan: plan, Profile: profile, CreatedAt: timeNow()}
		ensureProviderEnabled(cfg, providerType, true)
	default:
		apiKey := opts.apiKey
		if apiKey == "" {
			secret, err := promptSecret(i18n.T("prompt.api_key"))
			if err != nil {
				return err
			}
			apiKey = secret
		}
		if apiKey == "" {
			return errors.New(i18n.T("error.account.key_empty"))
		}

		accountName = opts.name
		if accountName == "" {
			name, err := promptInput(reader, i18n.T("prompt.account_name"))
			if err != nil {
				return err
			}
			accountName = name
		}
		if strings.ContainsAny(accountName, "/\n\x00:") {
			return errors.New(i18n.T("error.account.name_invalid"))
		}

		// Validate now so broken keys never reach the config.
		fmt.Println("  " + i18n.T("output.provider.add.validating_key"))
		if err := validateProviderKey(providerType, plan, apiKey); err != nil {
			return fmt.Errorf("%s", i18n.T("error.account.key_validation_failed", err))
		}

		if err := auth.StoreAPIKey("token-usage", providerType+"/"+accountName, apiKey); err != nil {
			return fmt.Errorf("failed to store API key: %w", err)
		}
		acc = config.Account{
			Source:    config.SourceManual,
			KeyID:     auth.ExtractKeyID(apiKey),
			Plan:      plan,
			Profile:   profile,
			CreatedAt: timeNow(),
		}
		ensureProviderEnabled(cfg, providerType, true)
	}

	if p.Accounts == nil {
		p.Accounts = map[string]config.Account{}
	}
	if _, exists := p.Accounts[accountName]; exists && acc.Source != config.SourceLocal {
		return fmt.Errorf("%s", i18n.T("error.account.already_exists", accountName, providerType))
	}
	// Adding an account (re)enables the provider.
	p.Enabled = true
	p.Accounts[accountName] = acc
	if p.DefaultAccount == "" {
		p.DefaultAccount = accountName
	}
	cfg.Providers[providerType] = p

	if err := config.SaveConfig(cfg, cfgPath); err != nil {
		if acc.Source == config.SourceManual {
			_ = auth.DeleteAPIKey("token-usage", providerType+"/"+accountName)
		}
		return err
	}

	fmt.Printf("%s", i18n.T("output.provider.add.preset_added", providerType, accountName, acc.Source))
	fmt.Println("  " + i18n.T("output.provider.add.run_quota"))
	return nil
}

// addVolcengineLocalAccounts registers local-source accounts for the
// Volcano entries found in opencode.json. Several entries (one per Volcano
// account, e.g. one per phone) become one account each, pinned to its
// opencode.json provider id via Account.OpencodeProvider.
func addVolcengineLocalAccounts(cfg *config.Config, cfgPath string, p config.PresetProvider, plan, profile string, opts addOpts) error {
	home, _ := os.UserHomeDir()
	credPath := p.CredentialPath("volcengine", home)
	entries, err := volcengineOpencodeEntries(credPath)
	if err != nil || len(entries) == 0 {
		return fmt.Errorf("%s", i18n.T("error.volcengine.no_entries", credPath))
	}

	reader := bufio.NewReader(os.Stdin)
	if opts.name != "" || opts.localRef != "" {
		chosen, err := pickVolcengineEntry(entries, opts, reader)
		if err != nil {
			return err
		}
		entries = []volcengineOpencodeEntry{chosen}
	} else if len(entries) > 1 {
		yes, err := promptYesNo(reader, "  "+i18n.T("prompt.volcengine.add_all", len(entries)), true)
		if err != nil {
			return err
		}
		if !yes {
			ids := make([]string, 0, len(entries))
			for _, e := range entries {
				ids = append(ids, e.ID)
			}
			idx, err := promptSelect(reader, i18n.T("prompt.volcengine.pick_entry"), ids)
			if err != nil {
				return err
			}
			entries = []volcengineOpencodeEntry{entries[idx]}
		}
	}

	if p.Accounts == nil {
		p.Accounts = map[string]config.Account{}
	}
	firstAdded := ""
	for _, e := range entries {
		name := opts.name
		if name == "" || len(entries) > 1 {
			name = deriveVolcengineAccountName(e.ID)
		}
		// Never double-bind one entry: two accounts on the same key would
		// render identical quota rows. Rebind by removing the old account
		// first.
		if owner := volcengineEntryOwner(p.Accounts, e.ID); owner != "" && owner != name {
			fmt.Printf("  %s\n", i18n.T("output.volcengine.entry_skipped", e.ID, owner))
			continue
		}
		acc := config.Account{
			Source:           config.SourceLocal,
			Plan:             plan,
			Profile:          profile,
			OpencodeProvider: e.ID,
			CreatedAt:        timeNow(),
		}
		// Re-adding refreshes the binding in place instead of piling up
		// duplicates; a name owned by a non-local account is rejected.
		if existing, ok := p.Accounts[name]; ok {
			if existing.Source != config.SourceLocal {
				return fmt.Errorf("%s", i18n.T("error.account.already_exists", name, "volcengine"))
			}
			acc.CreatedAt = existing.CreatedAt
			acc.LastVerified = existing.LastVerified
		}
		p.Accounts[name] = acc
		if firstAdded == "" {
			firstAdded = name
		}
		fmt.Printf("%s", i18n.T("output.volcengine.entry_added", name, e.ID))
	}

	p.Enabled = true
	if p.DefaultAccount == "" {
		p.DefaultAccount = firstAdded
	}
	cfg.Providers["volcengine"] = p
	if err := config.SaveConfig(cfg, cfgPath); err != nil {
		return err
	}

	if !provider.ArkcliAvailable() {
		fmt.Println()
		fmt.Println("  NOTE: arkcli is not installed. Keys will be probed for validity,")
		fmt.Println("  but full quota windows need the official CLI:")
		fmt.Println("    npm i -g @volcengine/ark-cli && arkcli auth login")
		fmt.Println("  (its installer injects skills into local AI agents; skip with ARKCLI_SKIP_POSTINSTALL=1)")
		fmt.Println()
	}
	fmt.Println("  " + i18n.T("output.provider.add.run_quota"))
	return nil
}

// volcengineEntryOwner returns the name of the local account already bound
// to the opencode.json entry, or "".
func volcengineEntryOwner(accounts map[string]config.Account, entryID string) string {
	for name, a := range accounts {
		if a.Source == config.SourceLocal && a.OpencodeProvider == entryID {
			return name
		}
	}
	return ""
}

// pickVolcengineEntry narrows the opencode.json entries to the one an
// explicit --name / --opencode-provider refers to.
func pickVolcengineEntry(entries []volcengineOpencodeEntry, opts addOpts, reader *bufio.Reader) (volcengineOpencodeEntry, error) {
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.ID)
	}
	list := strings.Join(ids, ", ")

	if opts.localRef != "" {
		for _, e := range entries {
			if e.ID == opts.localRef {
				return e, nil
			}
		}
		return volcengineOpencodeEntry{}, fmt.Errorf("%s", i18n.T("error.volcengine.entry_not_found", opts.localRef, list))
	}
	for _, e := range entries {
		if deriveVolcengineAccountName(e.ID) == opts.name {
			return e, nil
		}
	}
	if len(entries) == 1 {
		return entries[0], nil
	}
	return volcengineOpencodeEntry{}, fmt.Errorf("%s", i18n.T("error.volcengine.name_unmappable", opts.name, list))
}

// deriveVolcengineAccountName maps an opencode.json provider id to an
// account name ("Volcano-Engine-coding-plan-2" -> "coding-plan-2").
func deriveVolcengineAccountName(opencodeID string) string {
	n := strings.ToLower(opencodeID)
	n = strings.TrimPrefix(n, "volcano-engine-")
	n = strings.NewReplacer("/", "-", ":", "-", "\n", "").Replace(n)
	if n == "" {
		n = "local"
	}
	return n
}

// addCustomProvider collects name/type/base URL/key, validates the quota
// query live, and only persists on success.
func addCustomProvider(cfg *config.Config, cfgPath string, reader *bufio.Reader, opts addOpts) error {
	name := opts.name
	if name == "" {
		v, err := promptInput(reader, i18n.T("prompt.base_url"))
		if err != nil {
			return err
		}
		name = v
	}
	if strings.ContainsAny(name, " \n\x00:") {
		return errors.New(i18n.T("error.provider.name_spaces"))
	}
	if _, exists := cfg.Providers[name]; exists {
		return fmt.Errorf("%s", i18n.T("error.provider.name_conflicts", name))
	}
	if _, exists := cfg.Custom[name]; exists {
		return fmt.Errorf("%s", i18n.T("error.provider.custom_exists", name))
	}

	queryType := opts.queryType
	if queryType == "" {
		opts2 := make([]string, 0, len(provider.BuiltinKeyQueries))
		for _, qt := range provider.BuiltinKeyQueries {
			opts2 = append(opts2, qt)
		}
		idx, err := promptSelect(reader, i18n.T("prompt.query_impl"), opts2)
		if err != nil {
			return err
		}
		queryType = provider.BuiltinKeyQueries[idx]
	}
	if _, ok := provider.LookupKeyQuery(queryType); !ok {
		return fmt.Errorf("%s", i18n.T("error.provider.unknown_query_type", queryType, strings.Join(provider.BuiltinKeyQueries, ", ")))
	}

	baseURL := opts.baseURL
	if baseURL == "" {
		v, err := promptInput(reader, i18n.T("prompt.base_url"))
		if err != nil {
			return err
		}
		baseURL = v
	}
	baseURL = strings.TrimSuffix(baseURL, "/")

	apiKey := opts.apiKey
	if apiKey == "" {
		v, err := promptSecret(i18n.T("prompt.api_key"))
		if err != nil {
			return err
		}
		apiKey = v
	}
	if apiKey == "" {
		return errors.New(i18n.T("error.account.key_empty"))
	}

	// Live validation: the provider is only saved if quota querying works.
	fmt.Println("  " + i18n.T("output.provider.add.validating_query"))
	q, _ := provider.LookupKeyQuery(queryType)
	usage, err := q(apiKey, baseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n  %v\n", err)
		fmt.Fprintln(os.Stderr, "  "+i18n.T("output.provider.add.not_saved"))
		return errors.New(i18n.T("error.provider.validation_failed"))
	}

	accName := "main"
	if opts.name != "" && opts.name != name {
		accName = opts.name
	}

	cfg.Custom[name] = config.CustomProvider{
		QueryType:      queryType,
		BaseURL:        baseURL,
		DisplayName:    "",
		Enabled:        true,
		DefaultAccount: accName,
		Accounts: map[string]config.Account{
			accName: {
				Source:       config.SourceManual,
				KeyID:        auth.ExtractKeyID(apiKey),
				CreatedAt:    timeNow(),
				LastVerified: timeNow(),
			},
		},
	}
	if err := auth.StoreAPIKey("token-usage", name+"/"+accName, apiKey); err != nil {
		delete(cfg.Custom, name)
		return fmt.Errorf("failed to store API key: %w", err)
	}
	if err := config.SaveConfig(cfg, cfgPath); err != nil {
		_ = auth.DeleteAPIKey("token-usage", name+"/"+accName)
		delete(cfg.Custom, name)
		return err
	}

	fmt.Printf("%s", i18n.T("output.provider.add.custom_added", name, queryType))
	if usage.Note != "" {
		fmt.Printf("  %s\n", usage.Note)
	}
	return nil
}

// detectLocalAccount reports a human-readable description of the local
// credentials for a preset, or "" when none exist.
func detectLocalAccount(cfg *config.Config, providerType string) string {
	home, _ := os.UserHomeDir()
	switch providerType {
	case "claude":
		p := cfg.Providers["claude"]
		path := p.CredentialPath(providerType, home)
		if fileExists(path) {
			return "Claude Code login at " + path
		}
	case "codex":
		p := cfg.Providers["codex"]
		path := p.CredentialPath(providerType, home)
		if fileExists(path) {
			return "Codex login at " + path
		}
	case "volcengine":
		p := cfg.Providers["volcengine"]
		path := p.CredentialPath(providerType, home)
		if entries, err := volcengineOpencodeEntries(path); err == nil && len(entries) > 0 {
			return fmt.Sprintf("%d API key(s) in %s", len(entries), path)
		}
	}
	return ""
}

func ensureProviderEnabled(cfg *config.Config, providerType string, enabled bool) {
	p := cfg.Providers[providerType]
	p.Enabled = enabled
	cfg.Providers[providerType] = p
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

var providerRemoveCmd = &cobra.Command{
	Use:     "remove <provider>",
	Aliases: []string{"rm"},
	Short:   "Remove a custom provider or disable a preset",
	Long: `Remove a provider.

For custom providers this deletes the provider, all of its accounts, and
their stored API keys. Built-in presets are only disabled (their accounts
are kept); re-enable with 'token-usage provider enable <provider>'.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		cfgPath, err := getConfigPath()
		if err != nil {
			return err
		}
		cfg, err := config.LoadOrCreateConfig(cfgPath)
		if err != nil {
			return err
		}
		configureAuthFromConfig(cfg)

		if c, ok := cfg.Custom[id]; ok {
			for accName := range c.Accounts {
				_ = auth.DeleteAPIKey("token-usage", id+"/"+accName)
			}
			delete(cfg.Custom, id)
			if err := config.SaveConfig(cfg, cfgPath); err != nil {
				return err
			}
			fmt.Printf("%s", i18n.T("output.provider.remove.custom_removed", id)+"\n")
			return nil
		}
		if _, ok := cfg.Providers[id]; ok {
			ensureProviderEnabled(cfg, id, false)
			if err := config.SaveConfig(cfg, cfgPath); err != nil {
				return err
			}
			fmt.Printf("%s", i18n.T("output.provider.remove.preset_disabled", id, id)+"\n")
			return nil
		}
		return fmt.Errorf("%s", i18n.T("error.provider.not_found", id))
	},
}

var providerEnableCmd = &cobra.Command{
	Use:   "enable <provider>",
	Short: "Enable a provider",
	Long: `Enable a provider (preset or custom). Disabled providers are skipped
by quota/provider views and 'provider add' can also enable a preset.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return setProviderEnabled(args[0], true)
	},
}

var providerDisableCmd = &cobra.Command{
	Use:   "disable <provider>",
	Short: "Disable a provider",
	Long: `Disable a provider (preset or custom) without deleting its accounts.
Queries skip disabled providers until re-enabled.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return setProviderEnabled(args[0], false)
	},
}

func setProviderEnabled(id string, enabled bool) error {
	cfgPath, err := getConfigPath()
	if err != nil {
		return err
	}
	cfg, err := config.LoadOrCreateConfig(cfgPath)
	if err != nil {
		return err
	}
	if p, ok := cfg.Providers[id]; ok {
		p.Enabled = enabled
		cfg.Providers[id] = p
	} else if c, ok := cfg.Custom[id]; ok {
		c.Enabled = enabled
		cfg.Custom[id] = c
	} else {
		return fmt.Errorf("%s", i18n.T("error.provider.not_found", id))
	}
	if err := config.SaveConfig(cfg, cfgPath); err != nil {
		return err
	}
	state := i18n.T("output.provider.list.disabled")
	if enabled {
		state = i18n.T("output.provider.list.enabled")
	}
	fmt.Printf("Provider '%s' %s\n", id, state)
	return nil
}

func init() {
	providerAddCmd.Flags().StringVar(&addProviderType, "type", "", "provider type (opencode|claude|codex|volcengine|custom)")
	providerAddCmd.Flags().StringVar(&addAccountName, "name", "", "account/provider name")
	providerAddCmd.Flags().StringVar(&addQueryType, "query-type", "", "custom query implementation (zai-glm|kimi|minimax|deepseek|openai-compatible)")
	providerAddCmd.Flags().StringVar(&addBaseURL, "base-url", "", "custom provider base URL")
	providerAddCmd.Flags().StringVar(&addAPIKey, "key", "", "API key (prompts interactively when omitted)")
	providerAddCmd.Flags().StringVar(&addPlan, "plan", "", "volcengine plan (coding|agent)")
	providerAddCmd.Flags().BoolVar(&addUseLocal, "use-local", false, "reuse the locally detected account")
	providerAddCmd.Flags().StringVar(&addProfile, "profile", "", "volcengine arkcli profile (multi-account)")
	providerAddCmd.Flags().StringVar(&addLocalRef, "opencode-provider", "", "volcengine: opencode.json provider entry to read the key from (multi-account)")

	providerCmd.AddCommand(providerListCmd)
	providerCmd.AddCommand(providerAddCmd)
	providerCmd.AddCommand(providerRemoveCmd)
	providerCmd.AddCommand(providerEnableCmd)
	providerCmd.AddCommand(providerDisableCmd)
	rootCmd.AddCommand(providerCmd)
}
