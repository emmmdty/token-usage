package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/emmmdty/token-usage/internal/config"
	"github.com/emmmdty/token-usage/internal/i18n"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// resolveLanguage detects and applies the language setting, then refreshes
// all command descriptions and the help template. Runs before cobra.Execute
// so it works even for --help (which skips PersistentPreRun).
func resolveLanguage() {
	configLang := ""
	if cfgPath, err := getConfigPath(); err == nil {
		if cfg, err := config.LoadOrCreateConfig(cfgPath); err == nil {
			configLang = cfg.Language
		}
	}
	resolvedLang := i18n.DetectLanguage(langFlag, os.Getenv("TOKEN_USAGE_LANG"), configLang, os.Getenv("LANG"))
	i18n.SetLanguage(resolvedLang)

	// Update help template and command descriptions for current language
	installCustomHelp(rootCmd)
	refreshCmd(rootCmd)
}

func refreshCmd(cmd *cobra.Command) {
	// Map command names to i18n key prefixes
	keyPrefix := "cmd." + cmd.Name()
	if cmd.Name() == "token-usage" {
		keyPrefix = "cmd.root"
	}
	cmd.Short = i18n.T(keyPrefix + ".short")
	cmd.Long = i18n.T(keyPrefix + ".long")

	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		key := "flag." + f.Name
		if desc := i18n.T(key); desc != key {
			f.Usage = desc
		}
	})
	cmd.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		key := "flag." + f.Name
		if desc := i18n.T(key); desc != key {
			f.Usage = desc
		}
	})

	for _, child := range cmd.Commands() {
		refreshCmd(child)
	}
}

// installCustomHelp installs a help function that translates cobra framework
// labels (Usage:, Available Commands:, etc.) based on current language.
func installCustomHelp(cmd *cobra.Command) {
	cmd.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		w := cmd.OutOrStdout()

		// Long or Short description
		desc := cmd.Long
		if desc == "" {
			desc = cmd.Short
		}
		if desc != "" {
			fmt.Fprintln(w, desc)
			fmt.Fprintln(w)
		}

		// Usage line
		if cmd.Runnable() {
			fmt.Fprintf(w, "%s: %s\n", i18n.T("framework.usage"), cmd.UseLine())
		}

		// Aliases
		if len(cmd.Aliases) > 0 {
			fmt.Fprintf(w, "\n%s:", i18n.T("framework.aliases"))
			for _, a := range cmd.Aliases {
				fmt.Fprintf(w, " %s", a)
			}
			fmt.Fprintln(w)
		}

		// Available Commands
		commands := cmd.Commands()
		if len(commands) > 0 {
			fmt.Fprintf(w, "\n%s:\n", i18n.T("framework.available_commands"))
			for _, c := range commands {
				if !c.IsAvailableCommand() && c.Name() != "help" {
					continue
				}
				fmt.Fprintf(w, "  %-16s %s\n", c.Name(), c.Short)
			}
		}

		// Flags
		if cmd.HasLocalFlags() {
			fmt.Fprintf(w, "\n%s:\n", i18n.T("framework.flags"))
			fmt.Fprint(w, cmd.LocalFlags().FlagUsages())
		}

		// Global Flags
		if cmd.HasInheritedFlags() {
			fmt.Fprintf(w, "\n%s:\n", i18n.T("framework.global_flags"))
			fmt.Fprint(w, cmd.InheritedFlags().FlagUsages())
		}

		// Examples
		if cmd.HasExample() {
			fmt.Fprintf(w, "\n%s:\n%s\n", i18n.T("framework.examples"), cmd.Example)
		}

		// Help trail
		if cmd.HasParent() {
			fmt.Fprintf(w, "\n%s\n", strings.ReplaceAll(
				i18n.T("framework.help_trail"),
				"token-usage",
				cmd.Root().Name(),
			))
		}
	})
}
