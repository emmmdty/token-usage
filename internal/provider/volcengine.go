package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"errors"
	"github.com/emmmdty/token-usage/internal/i18n"
)

// Volcengine coding/agent plan endpoints (base of the ark API).
const volcengineAPIBase = "https://ark.cn-beijing.volces.com"

// VolcengineProvider queries Volcano Engine subscription quota.
//
// Two resolution paths:
//  1. arkcli (official CLI) when installed and logged in: `arkcli usage plan
//     --format json` returns the same windows as the console (session/weekly/
//     monthly for coding plan, 5h/weekly/monthly for agent plan). A non-empty
//     profile pins the query to that arkcli login, which is how multiple
//     Volcano accounts are told apart.
//  2. API-key probe: a max_tokens=1 chat completion confirms the key works;
//     rate-limit headers are unreliable on Ark (often absent on 200), so the
//     usage is reported as unknown with a note pointing at arkcli.
type VolcengineProvider struct {
	apiKey    string
	plan      string // "coding" | "agent"
	profile   string // arkcli profile name, "" = arkcli default profile
	arkcli    string // resolved path, "" = not installed
	probeBase string // override for tests
}

func NewVolcengineProvider(apiKey, plan, profile string) *VolcengineProvider {
	return &VolcengineProvider{
		apiKey:  apiKey,
		plan:    plan,
		profile: profile,
		arkcli:  lookPathArkcli(),
	}
}

// lookPathArkcli resolves the arkcli binary once per construction.
func lookPathArkcli() string {
	if path, err := exec.LookPath("arkcli"); err == nil {
		return path
	}
	return ""
}

// ArkcliAvailable reports whether the official ark CLI was found on PATH.
func ArkcliAvailable() bool {
	return lookPathArkcli() != ""
}

// ArkcliProfile describes one login profile known to the ark CLI. Each
// Volcano account login lands in its own profile, so listing them is how
// multi-account setups are discovered.
type ArkcliProfile struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Type        string `json:"type"` // coding-plan | agent-plan | platform | ...
	PlanTier    string `json:"plan_tier"`
	Region      string `json:"region"`
	IsDefault   bool   `json:"is_default"`
}

// ArkcliProfiles lists the ark CLI's login profiles via
// `arkcli profile list --format json`.
func ArkcliProfiles() ([]ArkcliProfile, error) {
	return arkcliProfiles(lookPathArkcli())
}

func arkcliProfiles(bin string) ([]ArkcliProfile, error) {
	if bin == "" {
		return nil, errors.New(i18n.T("provider.volcengine.arkcli_required"))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "profile", "list", "--format", "json")
	cmd.Env = arkcliEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("%s", i18n.T("provider.volcengine.profile_list_failed", truncateMsg(msg, 200)))
	}

	var out struct {
		Profiles []ArkcliProfile `json:"profiles"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return nil, fmt.Errorf("%s", i18n.T("provider.volcengine.unexpected_output", err))
	}
	return out.Profiles, nil
}

func (p *VolcengineProvider) Name() string {
	return "volcengine"
}

func (p *VolcengineProvider) IsAvailable() bool {
	return p.arkcli != "" || p.apiKey != ""
}

func (p *VolcengineProvider) GetUsage() (*Usage, error) {
	var arkErr error
	if p.arkcli != "" {
		usage, err := p.usageViaArkcli()
		if err == nil {
			return usage, nil
		}
		arkErr = err
		// Fall through to the probe when a key is available; otherwise
		// surface the arkcli error (e.g. not logged in).
		if p.apiKey == "" {
			return nil, fmt.Errorf("%s", i18n.T("provider.volcengine.arkcli_error", err))
		}
	}
	usage, err := p.usageViaProbe()
	if err == nil && arkErr != nil {
		// Make the silent fallback diagnosable instead of hiding it.
		usage.Note = fmt.Sprintf(i18n.T("provider.volcengine.note_arkcli_failed"), truncateMsg(arkErr.Error(), 100), usage.Note)
	}
	return usage, err
}

// arkcliEnv appends the update-suppression and caller-attribution variables
// to the inherited environment. Replacing os.Environ() entirely would break
// the arkcli wrapper (its node shebang needs PATH) and the CLI itself
// (it resolves its login state under $HOME).
func arkcliEnv() []string {
	return append(os.Environ(),
		"ARKCLI_NO_UPDATE_NOTIFIER=1",
		"ARKCLI_CALLER_TYPE=ai_agent",
		"ARKCLI_CALLER_NAME=token-usage",
	)
}

type arkcliPeriod struct {
	Label   string  `json:"label"`
	Percent float64 `json:"percent"`
	ResetAt string  `json:"reset_at"`
}

type arkcliItem struct {
	Product    string         `json:"product"`
	Edition    string         `json:"edition"`
	Subscribed bool           `json:"subscribed"`
	Periods    []arkcliPeriod `json:"periods"`
	Error      string         `json:"error"`
}

type arkcliOutput struct {
	Items []arkcliItem `json:"items"`
}

// pickArkcliItem selects the first subscribed item for a product, preferring
// personal edition over team seats.
func pickArkcliItem(out arkcliOutput, product string) *arkcliItem {
	var best *arkcliItem
	for i := range out.Items {
		item := &out.Items[i]
		if !item.Subscribed {
			continue
		}
		if item.Product != product && item.Product != product+"-team" {
			continue
		}
		if best != nil && best.Product == product && item.Product != product {
			continue // keep personal over team
		}
		best = item
	}
	return best
}

// arkcliArgs builds the argument list for a `usage plan` invocation. The
// profile is a root-level persistent flag, so it goes before the subcommand.
func (p *VolcengineProvider) arkcliArgs() []string {
	args := []string{}
	if p.profile != "" {
		args = append(args, "--profile", p.profile)
	}
	return append(args, "usage", "plan", "--format", "json")
}

// usageViaArkcli shells out to the official CLI. Read-only query; the local
// side effects are arkcli's own caches under ~/.arkcli.
func (p *VolcengineProvider) usageViaArkcli() (*Usage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, p.arkcli, p.arkcliArgs()...)
	cmd.Env = arkcliEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("%s", i18n.T("provider.volcengine.usage_plan_failed", truncateMsg(msg, 200)))
	}

	var out arkcliOutput
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return nil, fmt.Errorf("%s", i18n.T("provider.volcengine.unexpected_output", err))
	}

	usage := &Usage{
		Provider: "volcengine",
		PlanType: p.plan + "-plan",
		Rolling:  QuotaWindow{Status: StatusUnknown},
		Weekly:   QuotaWindow{Status: StatusUnknown},
		Monthly:  QuotaWindow{Status: StatusUnknown},
	}

	// Filter to items matching the configured plan, preferring personal
	// edition over team seats.
	wantPersonal := "agent-plan"
	if p.plan == PlanCoding {
		wantPersonal = "coding-plan"
	}
	best := pickArkcliItem(out, wantPersonal)
	if best == nil {
		return nil, fmt.Errorf("%s", i18n.T("provider.volcengine.no_subscription", wantPersonal))
	}

	usage.PlanType = wantPersonal
	if best.Edition != "" {
		usage.PlanType = wantPersonal + " (" + best.Edition + ")"
	}

	// Label mapping: coding plan uses "session", agent plan uses "5h".
	for _, period := range best.Periods {
		window := QuotaWindow{Status: "ok", Percent: int(period.Percent + 0.5)}
		if t, err := time.Parse(time.RFC3339, period.ResetAt); err == nil {
			window.ResetAt = t
		}
		switch period.Label {
		case "session", "5h":
			usage.Rolling = window
		case "weekly":
			usage.Weekly = window
		case "monthly":
			usage.Monthly = window
		}
	}
	return usage, nil
}

func truncateMsg(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// usageViaProbe validates the key with a 1-token completion and reads the
// rate-limit headers when Ark returns them. Without headers the windows are
// reported unknown; the note tells the user how to get full quota.
func (p *VolcengineProvider) usageViaProbe() (*Usage, error) {
	if p.apiKey == "" {
		return nil, errors.New(i18n.T("provider.volcengine.no_key_no_arkcli"))
	}

	base := p.probeBase
	if base == "" {
		base = volcengineAPIBase
	}

	payload := `{"model":"ark-code-latest","messages":[{"role":"user","content":"hi"}],"max_tokens":1}`
	req, err := http.NewRequest("POST", base+"/api/coding/v3/chat/completions", strings.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s", i18n.T("provider.volcengine.probe_failed", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusTooManyRequests {
		return nil, fmt.Errorf("%s", i18n.T("provider.volcengine.key_rejected", resp.StatusCode))
	}

	usage := &Usage{
		Provider: "volcengine",
		PlanType: p.plan + "-plan",
		Rolling:  QuotaWindow{Status: StatusUnknown},
		Weekly:   QuotaWindow{Status: StatusUnknown},
		Monthly:  QuotaWindow{Status: StatusUnknown},
	}

	limit := intHeader(resp.Header, "X-Ratelimit-Limit-Requests")
	remaining := intHeader(resp.Header, "X-Ratelimit-Remaining-Requests")
	if limit > 0 && remaining >= 0 {
		usage.Rolling = QuotaWindow{
			Status:  "ok",
			Percent: 100 - (remaining*100)/limit,
		}
		// X-Ratelimit-Reset-Requests reports an interval, not a point in
		// time, so no ResetAt can be derived from the headers.
		usage.Note = i18n.T("provider.volcengine.note_rate_limit")
	} else {
		usage.Note = i18n.T("provider.volcengine.note_key_valid")
	}
	return usage, nil
}

func intHeader(h http.Header, name string) int {
	v := h.Get(name)
	if v == "" {
		return -1
	}
	n := 0
	for _, r := range v {
		if r < '0' || r > '9' {
			return -1
		}
		n = n*10 + int(r-'0')
	}
	return n
}
