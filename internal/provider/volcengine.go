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
// Resolution order:
//  1. arkcli with an explicitly bound profile (Account.Profile) — full
//     windows for that exact login.
//  2. arkcli with an auto-matched profile: when the account's API key ends
//     with the same characters as a logged-in profile's masked key, that
//     profile provably belongs to the same account, so full windows are
//     safe. Without a match the key must NOT be queried through arkcli's
//     login (it could be a different account) and the probe is used.
//  3. API-key probe: a max_tokens=1 chat completion confirms the key works;
//     rate-limit headers are unreliable on Ark (often absent on 200), so
//     the usage is reported as unknown with a note pointing at arkcli.
type VolcengineProvider struct {
	apiKey     string
	plan       string // "coding" | "agent"
	profile    string // arkcli profile name, "" = auto-match or arkcli default
	arkcliHome string // alternate arkcli HOME (separate login), "" = real HOME
	arkcli     string // resolved path, "" = not installed
	probeBase  string // override for tests
}

func NewVolcengineProvider(apiKey, plan, profile, arkcliHome string) *VolcengineProvider {
	return &VolcengineProvider{
		apiKey:     apiKey,
		plan:       plan,
		profile:    profile,
		arkcliHome: arkcliHome,
		arkcli:     lookPathArkcli(),
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
	Name          string `json:"name"`
	DisplayName   string `json:"display_name"`
	Type          string `json:"type"` // coding-plan | agent-plan | platform | ...
	PlanTier      string `json:"plan_tier"`
	Region        string `json:"region"`
	IsDefault     bool   `json:"is_default"`
	DefaultAPIKey string `json:"default_api_key"` // masked, e.g. "ark****fa7d"
}

// matchProfileForKey resolves the arkcli profile for an API key in the
// given arkcli HOME. Variable so tests can stub it (the real lookup shells
// out to arkcli).
var matchProfileForKey = MatchArkcliProfileForKey

// MatchArkcliProfileForKey returns the name of the arkcli profile whose
// default API key provably belongs to the given key: arkcli masks keys in
// its profile listing ("ark****fa7d"), so a profile matches when its
// visible suffix equals the key's tail. Returns "" when nothing matches
// (or the match is ambiguous and no profile is marked default).
func MatchArkcliProfileForKey(apiKey, home string) string {
	profiles, err := ArkcliProfiles(home)
	if err != nil {
		return ""
	}
	return matchProfile(profiles, apiKey)
}

func matchProfile(profiles []ArkcliProfile, apiKey string) string {
	fallback := ""
	for _, pr := range profiles {
		suffix := maskedKeySuffix(pr.DefaultAPIKey)
		if suffix == "" || !strings.HasSuffix(apiKey, suffix) {
			continue
		}
		if pr.IsDefault {
			return pr.Name
		}
		if fallback == "" {
			fallback = pr.Name
		}
	}
	return fallback
}

// maskedKeySuffix extracts the visible tail of a masked key ("ark****fa7d"
// -> "fa7d"). Returns "" for values without a mask or without a tail.
func maskedKeySuffix(masked string) string {
	idx := strings.LastIndex(masked, "*")
	if idx < 0 || idx == len(masked)-1 {
		return ""
	}
	return masked[idx+1:]
}

// ArkcliProfiles lists the ark CLI's login profiles via
// `arkcli profile list --format json`, resolved in the given HOME
// ("" = the real HOME).
func ArkcliProfiles(home string) ([]ArkcliProfile, error) {
	return arkcliProfiles(lookPathArkcli(), home)
}

func arkcliProfiles(bin, home string) ([]ArkcliProfile, error) {
	if bin == "" {
		return nil, errors.New(i18n.T("provider.volcengine.arkcli_required"))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "profile", "list", "--format", "json")
	cmd.Env = arkcliEnv(home)
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
		profile := p.profile
		if profile == "" && p.apiKey != "" {
			// Ride the arkcli login only when the account's key provably
			// belongs to it; otherwise the probe below reports
			// identity-correct data instead of another account's quota.
			profile = matchProfileForKey(p.apiKey, p.arkcliHome)
		}
		if profile != "" || p.apiKey == "" {
			usage, err := p.usageViaArkcli(profile)
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
// the arkcli wrapper (its node shebang needs PATH) and the CLI itself.
// With a non-empty home, HOME/USERPROFILE are replaced (not appended: the
// first occurrence would win) so arkcli resolves its login state from that
// account's own HOME — the trick behind multi-account support.
func arkcliEnv(home string) []string {
	env := append([]string{}, os.Environ()...)
	env = append(env,
		"ARKCLI_NO_UPDATE_NOTIFIER=1",
		"ARKCLI_CALLER_TYPE=ai_agent",
		"ARKCLI_CALLER_NAME=token-usage",
	)
	if home != "" {
		env = replaceEnvValue(env, "HOME", home)
		env = replaceEnvValue(env, "USERPROFILE", home)
	}
	return env
}

// replaceEnvValue replaces every KEY=... entry's value, keeping position.
func replaceEnvValue(env []string, key, val string) []string {
	prefix := key + "="
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			env[i] = prefix + val
		}
	}
	return env
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
func (p *VolcengineProvider) arkcliArgs(profile string) []string {
	args := []string{}
	if profile != "" {
		args = append(args, "--profile", profile)
	}
	return append(args, "usage", "plan", "--format", "json")
}

// usageViaArkcli shells out to the official CLI. Read-only query; the local
// side effects are arkcli's own caches under ~/.arkcli.
func (p *VolcengineProvider) usageViaArkcli(profile string) (*Usage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, p.arkcli, p.arkcliArgs(profile)...)
	cmd.Env = arkcliEnv(p.arkcliHome)
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
		if p.arkcliHome != "" {
			usage.Note = i18n.T("provider.volcengine.note_key_valid_home", p.arkcliHome)
		} else {
			usage.Note = i18n.T("provider.volcengine.note_key_valid")
		}
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
