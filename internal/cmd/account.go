package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"errors"
	"github.com/emmmdty/token-usage/internal/auth"
	"github.com/emmmdty/token-usage/internal/config"
	"github.com/emmmdty/token-usage/internal/i18n"
	"github.com/emmmdty/token-usage/internal/provider"
	"github.com/mattn/go-runewidth"
	"github.com/spf13/cobra"
)

var accountCmd = &cobra.Command{
	Use:     "account",
	Aliases: []string{"a"},
	Short:   "Manage accounts per provider",
	Long: `Manage accounts within providers.

Every account belongs to a provider (e.g. opencode/work, claude/local).
Accounts with source "manual" keep their API key in the credential store;
accounts with source "local" are read from the tool's own login files
(Claude Code, Codex) or from opencode.json (Volcano Engine) at query time.

Run 'token-usage account add' to register a new account.`,
}

var (
	acctProvider string
	acctKey      string
	acctUseLocal bool
	acctPlan     string
	acctProfile  string
	acctLocalRef string
)

var accountAddCmd = &cobra.Command{
	Use:   "add [provider] [name]",
	Short: "Add an account to a provider",
	Long: `Add an account to a provider.

Presets (claude, codex, volcengine) detect local logins and offer to reuse
them without touching those files. With no arguments this is interactive.

For volcengine, each arkcli login profile is one Volcano account; bind a
profile (or provide its API key) per account to track several plans.

Non-interactive examples:
  token-usage account add opencode work --key sk-...
  token-usage account add volcengine --plan coding --use-local
  token-usage account add volcengine phone2 --profile coding-plan_cn-beijing_personal_2
  token-usage account add my-glm main --key zai-...   # custom provider`,
	Args: cobra.MaximumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		providerID := acctProvider
		name := ""
		if len(args) > 0 {
			providerID = args[0]
		}
		if len(args) > 1 {
			name = args[1]
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
		if providerID == "" {
			var known []string
			for _, pa := range cfg.AllAccounts() {
				known = appendUnique(known, pa.ProviderID)
			}
			for id := range cfg.Providers {
				known = appendUnique(known, id)
			}
			sort.Strings(known)
			idx, err := promptSelect(reader, "Provider:", known)
			if err != nil {
				return err
			}
			providerID = known[idx]
		}

		if _, _, err := cfg.FindProvider(providerID); err != nil {
			return fmt.Errorf("provider '%s' not found; add it first with 'token-usage provider add'", providerID)
		}

		if isCustom(cfg, providerID) {
			return addCustomAccount(cfg, cfgPath, reader, providerID, name)
		}
		return addPresetProvider(cfg, cfgPath, reader, providerID, addOpts{
			name:     name,
			apiKey:   acctKey,
			useLocal: acctUseLocal,
			plan:     acctPlan,
			profile:  acctProfile,
			localRef: acctLocalRef,
		})
	},
}

func appendUnique(list []string, v string) []string {
	for _, item := range list {
		if item == v {
			return list
		}
	}
	return append(list, v)
}

func isCustom(cfg *config.Config, id string) bool {
	_, ok := cfg.Custom[id]
	return ok
}

// addCustomAccount stores an additional API key under an existing custom
// provider, validating it live first.
func addCustomAccount(cfg *config.Config, cfgPath string, reader *bufio.Reader, providerID, name string) error {
	custom := cfg.Custom[providerID]
	q, _ := provider.LookupKeyQuery(custom.QueryType)

	apiKey := acctKey
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

	accountName := name
	if accountName == "" {
		v, err := promptInput(reader, i18n.T("prompt.account_name"))
		if err != nil {
			return err
		}
		accountName = v
	}
	if strings.ContainsAny(accountName, "/\n\x00:") {
		return errors.New(i18n.T("error.account.name_invalid"))
	}
	if _, exists := custom.Accounts[accountName]; exists {
		return fmt.Errorf("%s", i18n.T("error.account.already_exists", accountName, providerID))
	}

	fmt.Println("  " + i18n.T("output.provider.add.validating_query"))
	if _, err := q(apiKey, custom.BaseURL); err != nil {
		fmt.Fprintf(os.Stderr, "\n  %v\n", err)
		return errors.New(i18n.T("error.account.not_saved"))
	}

	if err := auth.StoreAPIKey("token-usage", providerID+"/"+accountName, apiKey); err != nil {
		return fmt.Errorf("failed to store API key: %w", err)
	}
	custom.Accounts[accountName] = config.Account{
		Source:       config.SourceManual,
		KeyID:        auth.ExtractKeyID(apiKey),
		CreatedAt:    timeNow(),
		LastVerified: timeNow(),
	}
	cfg.Custom[providerID] = custom
	if err := config.SaveConfig(cfg, cfgPath); err != nil {
		_ = auth.DeleteAPIKey("token-usage", providerID+"/"+accountName)
		return err
	}
	fmt.Printf("%s", i18n.T("output.account.add.added", accountName, providerID))
	return nil
}

var accountListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"al"},
	Short:   "List accounts grouped by provider",
	Long: `List accounts grouped by provider.

Each line shows provider/account, credential source, masked key id (manual
accounts only), verification status, and the last verified time. The arrow
(->) marks the provider's current account. Filter with --provider.`,
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

		return writeOutput(renderAccountList(cfg, acctProvider))
	},
}

func renderAccountList(cfg *config.Config, providerFilter string) string {
	// Group accounts by provider in stable order.
	type group struct {
		id       string
		display  string
		def      string
		accounts []config.ProviderAccount
	}
	groups := map[string]*group{}
	var order []string
	for _, pa := range cfg.AllAccounts() {
		if providerFilter != "" && pa.ProviderID != providerFilter {
			continue
		}
		g, ok := groups[pa.ProviderID]
		if !ok {
			g = &group{
				id:      pa.ProviderID,
				display: displayName(pa.ProviderID, pa.Data.Plan, customPtr(cfg, pa.ProviderID)),
				def:     defaultAccountFor(cfg, pa.ProviderID),
			}
			groups[pa.ProviderID] = g
			order = append(order, pa.ProviderID)
		}
		g.accounts = append(g.accounts, pa)
	}
	if len(order) == 0 {
		return i18n.T("output.account.list.empty") + "\n"
	}
	sort.Strings(order)

	nameWidth := 7
	for _, id := range order {
		for _, pa := range groups[id].accounts {
			w := runewidth.StringWidth(pa.ProviderID + "/" + pa.Account)
			if w+2 > nameWidth {
				nameWidth = w + 2
			}
		}
	}

	var out strings.Builder
	out.WriteString("\n")
	for _, id := range order {
		g := groups[id]
		fmt.Fprintf(&out, "  %s\n", g.display)
		for _, pa := range g.accounts {
			acc := pa.Data
			status := "unverified"
			lastVerified := "never"
			if !acc.LastVerified.IsZero() {
				lastVerified = formatRelativeTime(acc.LastVerified)
				if time.Since(acc.LastVerified) < 24*time.Hour {
					status = "ok"
				} else {
					status = "stale"
				}
			}
			marker := "  "
			if pa.Account == g.def {
				marker = "-> "
			}
			keyID := ""
			if acc.KeyID != "" {
				keyID = "  Key: sk-..." + acc.KeyID
			}
			profile := ""
			if acc.Profile != "" {
				profile = "  Profile: " + acc.Profile
			}
			padded := pa.ProviderID + "/" + pa.Account
			padded += strings.Repeat(" ", nameWidth-runewidth.StringWidth(padded))
			fmt.Fprintf(&out, "  %s%s  Source: %-5s %s  Status: %-10s  Last verified: %s%s\n",
				marker, padded, acc.Source, keyID, status, lastVerified, profile)
		}
		out.WriteString("\n")
	}
	return out.String()
}

var accountRemoveCmd = &cobra.Command{
	Use:     "remove <provider> <account>",
	Aliases: []string{"ar"},
	Short:   "Remove an account",
	Long: `Remove an account and its stored API key.

Accepted argument forms:
  token-usage account remove <provider> <account>
  token-usage account remove <provider>/<account>
  token-usage account remove <account>       # only when the name is unique

For custom providers with a single account this leaves the provider
defined but empty; use 'token-usage provider remove' to delete it fully.`,
	Args: cobra.MaximumNArgs(2),
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

		providerID, accountName, err := resolveProviderAccount(cfg, args)
		if err != nil {
			return err
		}

		if p, ok := cfg.Providers[providerID]; ok {
			delete(p.Accounts, accountName)
			if p.DefaultAccount == accountName {
				p.DefaultAccount = ""
			}
			cfg.Providers[providerID] = p
		} else if c, ok := cfg.Custom[providerID]; ok {
			delete(c.Accounts, accountName)
			if c.DefaultAccount == accountName {
				c.DefaultAccount = ""
			}
			cfg.Custom[providerID] = c
		}

		if err := config.SaveConfig(cfg, cfgPath); err != nil {
			return err
		}

		if err := auth.DeleteAPIKey("token-usage", providerID+"/"+accountName); err != nil {
			return fmt.Errorf("%s", i18n.T("error.account.key_delete_failed", err))
		}
		fmt.Printf("Account '%s/%s' removed\n", providerID, accountName)
		return nil
	},
}

// resolveProviderAccount turns CLI args into a (provider, account) pair.
// Accepts "provider account" or just "account" when the name is unique.
func resolveProviderAccount(cfg *config.Config, args []string) (string, string, error) {
	if len(args) == 2 {
		providerID, accountName := args[0], args[1]
		accounts, _, err := cfg.FindProvider(providerID)
		if err != nil {
			return "", "", err
		}
		if _, ok := accounts[accountName]; !ok {
			return "", "", fmt.Errorf("%s", i18n.T("error.account.not_found", accountName, providerID))
		}
		return providerID, accountName, nil
	}
	if len(args) == 1 {
		parts := strings.SplitN(args[0], "/", 2)
		if len(parts) == 2 {
			return resolveProviderAccount(cfg, parts)
		}
		var matches []config.ProviderAccount
		for _, pa := range cfg.AllAccounts() {
			if pa.Account == args[0] {
				matches = append(matches, pa)
			}
		}
		switch len(matches) {
		case 0:
			return "", "", fmt.Errorf("%s", i18n.T("error.account.not_found_simple", args[0]))
		case 1:
			return matches[0].ProviderID, matches[0].Account, nil
		default:
			var ids []string
			for _, m := range matches {
				ids = append(ids, m.ProviderID+"/"+m.Account)
			}
			return "", "", fmt.Errorf("%s", i18n.T("error.account.ambiguous", args[0], strings.Join(ids, ", ")))
		}
	}
	return "", "", errors.New(i18n.T("error.account.usage_remove"))
}

var accountSwitchCmd = &cobra.Command{
	Use:     "switch [provider] [account]",
	Aliases: []string{"sw"},
	Short:   "Mark an account as current",
	Long: `Mark an account as the current one for its provider.

Accepted argument forms (interactive picker when omitted):
  token-usage account switch                        # pick provider + account
  token-usage account switch <provider>             # pick account of provider
  token-usage account switch <provider> <account>
  token-usage account switch <provider>/<account>

For the opencode provider this also writes the key into opencode's own
auth.json (the classic switch), so the change applies to opencode after
running /connect there. Other providers only record the current marker used
by 'quota' and 'providers' output.`,
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

		// A single argument that names an existing provider means "pick an
		// account of that provider interactively".
		if len(args) == 1 && !strings.Contains(args[0], "/") {
			if accounts, _, err := cfg.FindProvider(args[0]); err == nil {
				names := sortedAccountNames(accounts)
				if len(names) == 0 {
					return fmt.Errorf("%s", i18n.T("error.account.no_accounts", args[0]))
				}
				if len(names) == 1 {
					args = []string{args[0], names[0]}
				} else {
					reader := bufio.NewReader(os.Stdin)
					opts := make([]string, len(names))
					def := defaultAccountFor(cfg, args[0])
					for i, n := range names {
						marker := ""
						if n == def {
							marker = i18n.T("output.account.switch.current_marker")
						}
						keyID := accounts[n].KeyID
						if keyID != "" {
							keyID = "  sk-..." + keyID
						}
						opts[i] = n + keyID + marker
					}
					idx, err := promptSelect(reader, "Switch account for '"+args[0]+"':", opts)
					if err != nil {
						return err
					}
					args = []string{args[0], names[idx]}
				}
			}
		}

		providerID, accountName, err := resolveProviderAccount(cfg, args)
		if err != nil {
			return err
		}

		// Record the marker.
		if p, ok := cfg.Providers[providerID]; ok {
			p.DefaultAccount = accountName
			cfg.Providers[providerID] = p
		} else if c, ok := cfg.Custom[providerID]; ok {
			c.DefaultAccount = accountName
			cfg.Custom[providerID] = c
		}
		if err := config.SaveConfig(cfg, cfgPath); err != nil {
			return err
		}

		fmt.Printf("%s", "  "+i18n.T("output.account.switch.current", providerID, accountName)+"\n")

		// opencode extra behavior: sync opencode auth.json.
		if providerID == "opencode" {
			apiKey, err := resolveStoredKey(providerID, accountName)
			if err != nil {
				return fmt.Errorf("failed to retrieve API key: %w", err)
			}
			providers, err := readAuthJSON()
			if err != nil {
				return err
			}
			providers["opencode-go"] = authProvider{Type: "api", Key: apiKey}
			if err := writeAuthJSON(providers); err != nil {
				return err
			}
			fmt.Printf("%s", "  "+i18n.T("output.account.switch.auth_updated", auth.ExtractKeyID(apiKey))+"\n")
			if switchClipboard {
				if err := copyToClipboard(apiKey); err != nil {
					fmt.Printf("%s", "  "+i18n.T("output.account.switch.clipboard_error", err)+"\n")
				} else {
					fmt.Println("  " + i18n.T("output.account.switch.clipboard_ok"))
					fmt.Println("  " + i18n.T("output.account.switch.clipboard_warn"))
				}
			}
			fmt.Println("  " + i18n.T("output.account.switch.run_connect"))
		}
		return nil
	},
}

var accountTestCmd = &cobra.Command{
	Use:     "test [provider] [account]",
	Aliases: []string{"t"},
	Short:   "Validate that quota querying works for an account",
	Long: `Validate that quota querying works for an account.

Performs one live quota query and prints the resolved windows plus any
provider note (e.g. arkcli hints). On success the account's last-verified
timestamp is refreshed.

Accepted argument forms are the same as 'account remove' (pair, slash form,
or unique bare account name).`,
	Args: cobra.MaximumNArgs(2),
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

		providerID, accountName, err := resolveProviderAccount(cfg, args)
		if err != nil {
			return err
		}

		targets, _, err := buildTargets(cfg, providerID, accountName)
		if err != nil {
			return err
		}
		if len(targets) == 0 {
			return fmt.Errorf("%s", i18n.T("error.account.cannot_build_query", providerID, accountName))
		}

		fmt.Printf("%s", "  "+i18n.T("output.account.test.testing", providerID, accountName)+"\n")
		usage, err := targets[0].query()
		if err != nil {
			return fmt.Errorf("%s", i18n.T("output.account.test.failed", err))
		}
		touchLastVerified(cfg, providerID, accountName, cfgPath)
		fmt.Printf("  "+i18n.T("output.account.test.ok")+"\n",
			usage.PlanType,
			windowPct(usage.Rolling), windowPct(usage.Weekly), windowPct(usage.Monthly))
		if usage.Note != "" {
			fmt.Printf("  %s\n", usage.Note)
		}
		return nil
	},
}

func windowPct(w provider.QuotaWindow) string {
	if w.Status == provider.StatusUnknown {
		return "n/a"
	}
	return strconv.Itoa(w.Percent)
}

type ExportAccount struct {
	Provider string `json:"provider"`
	Name     string `json:"name"`
	KeyID    string `json:"key_id"`
}

type ExportData struct {
	Accounts []ExportAccount `json:"accounts"`
}

type ImportAccount struct {
	Provider string `json:"provider,omitempty"`
	Name     string `json:"name"`
	APIKey   string `json:"api_key"`
}

type ImportData struct {
	Accounts []ImportAccount `json:"accounts"`
}

var accountExportCmd = &cobra.Command{
	Use:     "export",
	Aliases: []string{"ae"},
	Short:   "Export account metadata (no secrets)",
	Long: `Export account metadata as JSON.

The output contains provider id, account name, and masked key id only —
API keys are never written. Use with --output to write to a file.`,
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

		var exportData ExportData
		for _, pa := range cfg.AllAccounts() {
			exportData.Accounts = append(exportData.Accounts, ExportAccount{
				Provider: pa.ProviderID,
				Name:     pa.Account,
				KeyID:    pa.Data.KeyID,
			})
		}
		if len(exportData.Accounts) == 0 {
			return fmt.Errorf("no accounts configured")
		}

		jsonData, err := json.MarshalIndent(exportData, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to serialize JSON: %w", err)
		}
		if outputFile != "" {
			if err := os.WriteFile(outputFile, jsonData, 0600); err != nil {
				return fmt.Errorf("%s", i18n.T("error.config.file_write", err))
			}
			fmt.Printf("%s", i18n.T("output.account.export.exported", outputFile)+"\n")
		} else {
			fmt.Println(string(jsonData))
		}
		return nil
	},
}

var accountImportCmd = &cobra.Command{
	Use:     "import <file>",
	Aliases: []string{"ai"},
	Short:   "Import accounts from a JSON file",
	Long: `Import accounts from a JSON file.

Expected format (api_key is required, provider defaults to opencode):
  {
    "accounts": [
      {"provider": "opencode", "name": "work", "api_key": "sk-..."},
      {"name": "legacy", "api_key": "sk-..."}
    ]
  }

Entries with missing names/keys, unknown providers, or duplicate accounts
are skipped with a reason; keys are stored before the config is updated.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := os.ReadFile(args[0])
		if err != nil {
			return fmt.Errorf("failed to read file: %w", err)
		}
		var importData ImportData
		if err := json.Unmarshal(data, &importData); err != nil {
			return fmt.Errorf("failed to parse JSON: %w", err)
		}
		if len(importData.Accounts) == 0 {
			return errors.New(i18n.T("error.account.import_no_accounts"))
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

		var imported, skipped int
		for _, entry := range importData.Accounts {
			providerID := entry.Provider
			if providerID == "" {
				providerID = "opencode" // legacy exports were opencode keys
			}
			if entry.Name == "" || entry.APIKey == "" {
				fmt.Printf("%s", i18n.T("output.account.import.skipping_invalid", entry.Name)+"\n")
				skipped++
				continue
			}
			if strings.ContainsAny(entry.Name, "/\n\x00:") {
				fmt.Printf("%s", i18n.T("output.account.import.skipping_chars", entry.Name)+"\n")
				skipped++
				continue
			}

			accounts, _, err := cfg.FindProvider(providerID)
			if err != nil {
				fmt.Printf("%s", i18n.T("output.account.import.skipping_provider", entry.Name, providerID)+"\n")
				skipped++
				continue
			}
			if _, exists := accounts[entry.Name]; exists {
				fmt.Printf("%s", i18n.T("output.account.import.skipping_existing", providerID, entry.Name)+"\n")
				skipped++
				continue
			}

			if err := auth.StoreAPIKey("token-usage", providerID+"/"+entry.Name, entry.APIKey); err != nil {
				fmt.Printf("%s", i18n.T("output.account.import.skipping_storage", entry.Name, err)+"\n")
				skipped++
				continue
			}

			acc := config.Account{
				Source:    config.SourceManual,
				KeyID:     auth.ExtractKeyID(entry.APIKey),
				CreatedAt: timeNow(),
			}
			if p, ok := cfg.Providers[providerID]; ok {
				if p.Accounts == nil {
					p.Accounts = map[string]config.Account{}
				}
				p.Accounts[entry.Name] = acc
				cfg.Providers[providerID] = p
			} else if c, ok := cfg.Custom[providerID]; ok {
				c.Accounts[entry.Name] = acc
				cfg.Custom[providerID] = c
			}
			if err := config.SaveConfig(cfg, cfgPath); err != nil {
				_ = auth.DeleteAPIKey("token-usage", providerID+"/"+entry.Name)
				fmt.Printf("%s", i18n.T("output.account.import.skipping_config", entry.Name, err)+"\n")
				skipped++
				continue
			}
			fmt.Printf("%s", i18n.T("output.account.import.imported", providerID, entry.Name)+"\n")
			imported++
		}
		fmt.Printf("%s", i18n.T("output.account.import.complete", imported, skipped))
		return nil
	},
}

func readAuthJSON() (map[string]authProvider, error) {
	data, err := os.ReadFile(opencodeAuthPath())
	if err != nil {
		return nil, fmt.Errorf("%s", i18n.T("error.account.auth_json_read", err))
	}
	var providers map[string]authProvider
	if err := json.Unmarshal(data, &providers); err != nil {
		return nil, fmt.Errorf("%s", i18n.T("error.account.auth_json_parse", err))
	}
	return providers, nil
}

func writeAuthJSON(providers map[string]authProvider) error {
	authDir := filepathDir(opencodeAuthPath())
	if err := os.MkdirAll(authDir, 0700); err != nil {
		return fmt.Errorf("%s", i18n.T("error.config.file_create_dir", err))
	}
	data, err := json.MarshalIndent(providers, "", "  ")
	if err != nil {
		return fmt.Errorf("%s", i18n.T("error.config.file_marshal", err))
	}
	tmpFile, err := os.CreateTemp(authDir, "auth.json.tmp.*")
	if err != nil {
		return fmt.Errorf("%s", i18n.T("error.config.file_temp", err))
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("%s", i18n.T("error.config.file_temp_write", err))
	}
	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("%s", i18n.T("error.config.file_temp_sync", err))
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("%s", i18n.T("error.config.file_temp_close", err))
	}
	if err := os.Chmod(tmpPath, 0600); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("%s", i18n.T("error.config.file_perms", err))
	}
	if err := os.Rename(tmpPath, opencodeAuthPath()); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("%s", i18n.T("error.config.file_replace", err))
	}
	return nil
}

var switchClipboard bool

func copyToClipboard(text string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "linux":
		if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.Command("xclip", "-selection", "clipboard")
		} else if _, err := exec.LookPath("xsel"); err == nil {
			cmd = exec.Command("xsel", "--clipboard", "--input")
		} else if _, err := exec.LookPath("clip.exe"); err == nil {
			cmd = exec.Command("clip.exe")
		} else {
			return errors.New(i18n.T("error.account.clipboard_not_found"))
		}
	case "windows":
		cmd = exec.Command("clip")
	default:
		return fmt.Errorf("%s", i18n.T("error.account.clipboard_unsupported", runtime.GOOS))
	}
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

var clearClipboardCmd = &cobra.Command{
	Use:     "clear-clipboard",
	Aliases: []string{"clc"},
	Short:   "Clear the system clipboard",
	Long:    "Clear the system clipboard to remove any API key that was copied by 'account switch --clipboard'.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := copyToClipboard(""); err != nil {
			return fmt.Errorf("%s", i18n.T("error.account.clear_clipboard_failed", err))
		}
		fmt.Println("  " + i18n.T("output.account.clear_clipboard.cleared"))
		return nil
	},
}

func init() {
	accountAddCmd.Flags().StringVarP(&acctProvider, "provider", "p", "", "target provider")
	accountAddCmd.Flags().StringVar(&acctKey, "key", "", "API key (prompts interactively when omitted)")
	accountAddCmd.Flags().BoolVar(&acctUseLocal, "use-local", false, "reuse the locally detected account")
	accountAddCmd.Flags().StringVar(&acctPlan, "plan", "", "volcengine plan (coding|agent)")
	accountAddCmd.Flags().StringVar(&acctProfile, "profile", "", "volcengine arkcli profile (multi-account)")
	accountAddCmd.Flags().StringVar(&acctLocalRef, "opencode-provider", "", "volcengine: opencode.json provider entry to read the key from (multi-account)")
	accountListCmd.Flags().StringVarP(&acctProvider, "provider", "p", "", "filter by provider")

	accountCmd.AddCommand(accountAddCmd)
	accountCmd.AddCommand(accountListCmd)
	accountCmd.AddCommand(accountRemoveCmd)
	accountCmd.AddCommand(accountSwitchCmd)
	accountCmd.AddCommand(accountTestCmd)
	accountCmd.AddCommand(accountExportCmd)
	accountCmd.AddCommand(accountImportCmd)
	accountCmd.AddCommand(clearClipboardCmd)
	accountSwitchCmd.Flags().BoolVarP(&switchClipboard, "clipboard", "c", false, "copy API key to clipboard after switching (opencode only)")
	rootCmd.AddCommand(accountCmd)
}
