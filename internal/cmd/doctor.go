package cmd

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/emmmdty/token-usage/internal/auth"
	"github.com/emmmdty/token-usage/internal/config"
	"github.com/emmmdty/token-usage/internal/i18n"
	"github.com/emmmdty/token-usage/internal/provider"
	"github.com/spf13/cobra"
)

// countProviders counts enabled providers (preset + custom).
func countProviders(cfg *config.Config) int {
	n := 0
	for _, p := range cfg.Providers {
		if p.Enabled {
			n++
		}
	}
	for _, c := range cfg.Custom {
		if c.Enabled {
			n++
		}
	}
	return n
}

// doctorNetworkProbe is the connectivity check; a variable so tests can
// stub the network.
var doctorNetworkProbe = func() error {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("https://opencode.ai/zen/go/v1/usage")
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// Stubbable arkcli probes for the per-HOME SSO check.
var (
	doctorArkcliAvailable  = provider.ArkcliAvailable
	doctorArkcliHomeStatus = provider.ArkcliHomeLoginState
)

// checkArkcliSso appends one WARN per volcengine account whose arkcli HOME
// lost its volc-sso login (the server eventually rejects the refresh token
// that renews arkcli's short-lived STS credentials, degrading quota queries
// to n/a). Healthy accounts add no line. AK/SK profiles are exempt: the
// sso_expired flag is identity-level and irrelevant when queries are signed
// with a permanent access key.
func checkArkcliSso(cfg *config.Config, checks []doctorCheck) []doctorCheck {
	if !doctorArkcliAvailable() {
		return checks
	}
	volc, ok := cfg.Providers["volcengine"]
	if !ok || !volc.Enabled {
		return checks
	}
	names := make([]string, 0, len(volc.Accounts))
	for name := range volc.Accounts {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		acc := volc.Accounts[name]
		st, err := doctorArkcliHomeStatus(acc.ArkcliHome, acc.Profile)
		if err == nil && (!st.SsoExpired || st.AuthMethod == "aksk") {
			continue
		}
		var detail string
		if err != nil {
			detail = i18n.T("output.doctor.arkcli_sso_error", "volcengine/"+name, err)
		} else if acc.ArkcliHome != "" {
			detail = i18n.T("output.doctor.arkcli_sso_expired", "volcengine/"+name, acc.ArkcliHome)
		} else {
			detail = i18n.T("output.doctor.arkcli_sso_expired_default", "volcengine/"+name)
		}
		checks = append(checks, doctorCheck{name: i18n.T("output.doctor.arkcli_sso"), status: "WARN", detail: detail})
	}
	return checks
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose configuration and connectivity issues",
	Long: `Diagnose configuration and connectivity issues.

Checks: config file readability, account/provider counts, credential store
(keyring or encrypted fallback), arkcli availability (full Volcano Engine
quota windows), network reachability, and opencode's auth.json presence.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		theme := newDoctorTheme()
		checks := []doctorCheck{}

		configPath, err := getConfigPath()
		if err != nil {
			checks = append(checks, doctorCheck{name: i18n.T("output.doctor.config_path"), status: "FAIL", detail: err.Error()})
		} else {
			cfg, err := config.LoadOrCreateConfig(configPath)
			if err != nil {
				checks = append(checks, doctorCheck{name: i18n.T("output.doctor.config_file"), status: "FAIL", detail: err.Error()})
			} else {
				configureAuthFromConfig(cfg)
				checks = append(checks, doctorCheck{name: i18n.T("output.doctor.config_file"), status: "OK", detail: configPath})
				accounts := cfg.AllAccounts()
				checks = append(checks, doctorCheck{
					name:   i18n.T("output.doctor.accounts"),
					status: "OK",
					detail: i18n.T("output.doctor.accounts_detail", len(accounts), countProviders(cfg)),
				})
				// arkcli availability matters for full Volcano quota windows.
				if provider.ArkcliAvailable() {
					checks = append(checks, doctorCheck{name: i18n.T("output.doctor.arkcli"), status: "OK", detail: i18n.T("output.doctor.arkcli_ok")})
				} else {
					checks = append(checks, doctorCheck{
						name:   i18n.T("output.doctor.arkcli"),
						status: "WARN",
						detail: i18n.T("output.doctor.arkcli_warn"),
					})
				}
				checks = checkArkcliSso(cfg, checks)
			}
		}

		if auth.IsKeyringAvailable() {
			checks = append(checks, doctorCheck{name: i18n.T("output.doctor.keyring"), status: "OK", detail: i18n.T("output.doctor.keyring_ok")})
		} else {
			checks = append(checks, doctorCheck{name: i18n.T("output.doctor.keyring"), status: "WARN", detail: i18n.T("output.doctor.keyring_warn")})
		}

		if doctorNetworkProbe() != nil {
			checks = append(checks, doctorCheck{name: i18n.T("output.doctor.network"), status: "FAIL", detail: i18n.T("output.doctor.network_fail")})
		} else {
			checks = append(checks, doctorCheck{name: i18n.T("output.doctor.network"), status: "OK", detail: i18n.T("output.doctor.network_ok")})
		}

		homeDir, err := os.UserHomeDir()
		if err != nil {
			checks = append(checks, doctorCheck{name: i18n.T("output.doctor.opencode_auth"), status: "WARN", detail: err.Error()})
		} else {
			authPath := filepath.Join(homeDir, ".local", "share", "opencode", "auth.json")
			if _, err := os.Stat(authPath); err == nil {
				checks = append(checks, doctorCheck{name: i18n.T("output.doctor.opencode_auth"), status: "OK", detail: i18n.T("output.doctor.opencode_auth_ok")})
			} else {
				checks = append(checks, doctorCheck{name: i18n.T("output.doctor.opencode_auth"), status: "WARN", detail: i18n.T("output.doctor.opencode_auth_warn")})
			}
		}

		var out strings.Builder
		for _, c := range checks {
			icon := theme.okIcon
			switch c.status {
			case "WARN":
				icon = theme.warnIcon
			case "FAIL":
				icon = theme.failIcon
			}
			fmt.Fprintf(&out, "  %s %-20s %s\n", icon, c.name, c.detail)
		}

		allOK := true
		failCount := 0
		for _, c := range checks {
			if c.status == "FAIL" {
				allOK = false
				failCount++
			}
		}
		if allOK {
			fmt.Fprintln(&out, "\n  "+i18n.T("output.doctor.all_passed"))
		} else {
			fmt.Fprintln(&out, "\n  "+i18n.T("output.doctor.some_failed"))
		}
		if err := writeOutput(out.String()); err != nil {
			return err
		}
		// Surface failures through the exit code so scripts and CI can act
		// on the verdict (warnings deliberately do not fail).
		if failCount > 0 {
			return fmt.Errorf("%s", i18n.T("error.doctor.checks_failed", failCount))
		}
		return nil
	},
}

type doctorCheck struct {
	name   string
	status string
	detail string
}

type doctorTheme struct {
	okIcon   string
	warnIcon string
	failIcon string
}

func newDoctorTheme() doctorTheme {
	if noColor || os.Getenv("NO_COLOR") != "" || !isTerminal() {
		return doctorTheme{okIcon: "[OK]", warnIcon: "[!!]", failIcon: "[XX]"}
	}
	return doctorTheme{okIcon: "\033[32m✓\033[0m", warnIcon: "\033[33m!\033[0m", failIcon: "\033[31m✗\033[0m"}
}

func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}
