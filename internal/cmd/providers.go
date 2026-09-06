package cmd

import (
	"fmt"

	"github.com/emmmdty/token-usage/internal/config"
	"github.com/emmmdty/token-usage/internal/tui"
	"github.com/spf13/cobra"
)

var providersCmd = &cobra.Command{
	Use:     "providers [provider]",
	Aliases: []string{"p"},
	Short:   "View usage grouped by provider (OpenCode, Claude, Codex, Volcano Engine, custom)",
	Long: `View usage across providers; this is the default view when running
'token-usage' with no subcommand.

Accepts an optional provider filter (e.g. 'token-usage providers volcengine').
Every enabled account of every provider is queried concurrently; unknown
windows render as n/a with provider notes underneath. Use --json for
machine-readable output.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		providerFilter := ""
		if len(args) > 0 {
			providerFilter = args[0]
		}
		return runProvidersOverview(providerFilter, jsonOutput, outputFile)
	},
}

func runProvidersOverview(providerFilter string, jsonOut bool, outPath string) error {
	configPath, err := getConfigPath()
	if err != nil {
		return err
	}

	cfg, err := config.LoadOrCreateConfig(configPath)
	if err != nil {
		return err
	}

	configureAuthFromConfig(cfg)

	// Same engine as quota; the only difference is the default filter scope
	// (all providers) and grouped rendering.
	if _, _, err := cfg.FindProvider(providerFilter); providerFilter != "" && err != nil {
		return fmt.Errorf("provider '%s' not found or not configured", providerFilter)
	}

	results, err := fetchAndRender(cfg, providerFilter, "", jsonOut)
	if err != nil {
		return err
	}

	if jsonOut {
		resp := providersResponse{Version: "2", Providers: results}
		return printJSON(resp)
	}
	return printProvidersTable(results, cfg)
}

type providerResult = accountResult

type providersResponse struct {
	Version   string           `json:"version"`
	Providers []providerResult `json:"providers"`
}

func printProvidersTable(results []providerResult, cfg *config.Config) error {
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

func init() {
	rootCmd.AddCommand(providersCmd)
}
