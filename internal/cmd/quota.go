package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/emmmdty/token-usage/internal/config"
	"github.com/emmmdty/token-usage/internal/i18n"
	"github.com/emmmdty/token-usage/internal/models"
	"github.com/emmmdty/token-usage/internal/provider"
	"github.com/emmmdty/token-usage/internal/tui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type accountResult struct {
	Provider        string        `json:"provider"`
	ProviderCode    string        `json:"provider_code,omitempty"`
	ProviderDisplay string        `json:"provider_display,omitempty"`
	Account         string        `json:"account"`
	Name            string        `json:"name"`
	PlanType        string        `json:"plan_type,omitempty"`
	Usage           *models.Usage `json:"quota,omitempty"`
	Error           string        `json:"error,omitempty"`
	IsCurrent       bool          `json:"is_current,omitempty"`
}

type quotaResponse struct {
	Version  string          `json:"version"`
	Accounts []accountResult `json:"accounts"`
}

var quotaCmd = &cobra.Command{
	Use:     "quota [account]",
	Aliases: []string{"q"},
	Short:   "View quota usage for all configured accounts",
	Long: `View quota usage for every enabled provider account.

Accepts an optional filter, in any of these forms:
  token-usage quota                    # everything
  token-usage quota <account>          # unique bare account name
  token-usage quota <provider>         # whole provider (e.g. volcengine)
  token-usage quota <provider>/<account>
The -n/--account global flag accepts the same forms.

Windows that a provider cannot resolve are shown as n/a; probe-only
providers (e.g. Volcano without arkcli) print a note under their row.
Use --json for machine-readable output.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		accountFilter := account
		if len(args) > 0 {
			accountFilter = args[0]
		}
		return runQuotaOverview(accountFilter, jsonOutput, outputFile)
	},
}

// fetchAndRender resolves targets, queries them concurrently, and hands the
// results to the chosen renderer.
func fetchAndRender(cfg *config.Config, providerFilter, accountFilter string, jsonOut bool) ([]accountResult, error) {
	targets, notes, err := buildTargets(cfg, providerFilter, accountFilter)
	if err != nil {
		return nil, err
	}

	if len(targets) == 0 {
		var extra string
		for _, n := range notes {
			extra += "\n  · " + n
		}
		if jsonOut {
			if extra != "" {
				fmt.Fprintln(os.Stderr, extra)
			}
			// Return an empty (non-nil) slice: the caller prints the JSON
			// exactly once, and an empty result must encode as [] not null.
			return []accountResult{}, nil
		}
		return nil, fmt.Errorf("%s", i18n.T("output.quota.no_accounts", extra))
	}

	if !jsonOut && term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Fprintf(os.Stderr, i18n.T("output.quota.fetching"), len(targets))
	}

	results := runQueryTargets(cfg, targets)

	out := make([]accountResult, 0, len(results))
	for _, r := range results {
		ar := accountResult{
			Provider:        r.Target.ProviderID,
			ProviderCode:    r.Target.ProviderCode,
			ProviderDisplay: r.Target.Display,
			Account:         r.Target.Account,
			Name:            r.Target.Name,
			IsCurrent:       r.Target.IsCurrent,
		}
		if r.Err != nil {
			ar.Error = r.Err.Error()
		} else {
			ar.Usage = models.FromProviderUsage(r.Usage)
			ar.PlanType = r.Usage.PlanType
		}
		out = append(out, ar)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Provider != out[j].Provider {
			return out[i].Provider < out[j].Provider
		}
		return out[i].Account < out[j].Account
	})
	return out, nil
}

func runQuotaOverview(accountFilter string, jsonOut bool, outPath string) error {
	configPath, err := getConfigPath()
	if err != nil {
		return err
	}
	cfg, err := config.LoadOrCreateConfig(configPath)
	if err != nil {
		return err
	}
	configureAuthFromConfig(cfg)

	// Accept "provider/account", a bare provider name, or a bare account
	// name as filters.
	providerFilter, accountFilter := splitQuotaFilter(cfg, accountFilter)

	results, err := fetchAndRender(cfg, providerFilter, accountFilter, jsonOut)
	if err != nil {
		return err
	}

	if jsonOut {
		resp := quotaResponse{Version: "2", Accounts: results}
		return printJSON(resp)
	}
	return printQuotaTable(results, cfg)
}

// splitQuotaFilter resolves a quota filter argument into
// (providerFilter, accountFilter). "provider/account" splits on the first
// slash; a bare name selects the whole provider when it matches one
// (preset or custom), and is treated as an account name otherwise.
func splitQuotaFilter(cfg *config.Config, arg string) (string, string) {
	if idx := strings.Index(arg, "/"); idx >= 0 {
		return arg[:idx], arg[idx+1:]
	}
	if _, _, err := cfg.FindProvider(arg); err == nil {
		return arg, ""
	}
	return "", arg
}

func printJSON(data interface{}) error {
	var buf strings.Builder
	encoder := json.NewEncoder(&buf)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(data); err != nil {
		return err
	}
	return writeOutput(buf.String())
}

func printQuotaTable(results []accountResult, cfg *config.Config) error {
	style := tui.QuotaStyle{
		WarningThreshold: cfg.ColorThresholds.Warning,
		DangerThreshold:  cfg.ColorThresholds.Danger,
	}
	if style.WarningThreshold == 0 {
		style.WarningThreshold = 50
	}
	if style.DangerThreshold == 0 {
		style.DangerThreshold = 80
	}

	output := tui.FormatQuotaOverview(toTuiResults(results), style, "")
	return writeOutput(output)
}

// toTuiResults maps query results to the render model, carrying the
// provider code/display split and the plain account label.
func toTuiResults(results []accountResult) []tui.AccountResult {
	tuiResults := make([]tui.AccountResult, len(results))
	for i, r := range results {
		var note string
		if r.Usage != nil {
			note = r.Usage.Note
		}
		tuiResults[i] = tui.AccountResult{
			Name:            r.Name,
			ProviderCode:    r.ProviderCode,
			ProviderDisplay: r.ProviderDisplay,
			AccountLabel:    r.Account,
			Usage:           r.Usage,
			Error:           r.Error,
			Note:            note,
			IsCurrent:       r.IsCurrent,
		}
	}
	return tuiResults
}

// makeProviderResult converts a provider.Usage into the JSON shape with the
// provider-specific windows already mapped.
func convertUsage(u *provider.Usage) *models.Usage {
	return models.FromProviderUsage(u)
}

func init() {
	rootCmd.AddCommand(quotaCmd)
}
