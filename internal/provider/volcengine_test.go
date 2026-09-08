package provider

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/emmmdty/token-usage/internal/i18n"
)

func TestVolcengineProbe_Ke_valid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/coding/v3/chat/completions" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-api-key" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		// Real Ark 200 responses typically omit rate-limit headers.
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"role": "assistant", "content": ""}},
			},
		})
	}))
	defer server.Close()

	p := &VolcengineProvider{apiKey: "test-api-key", plan: PlanCoding, probeBase: server.URL}
	usage, err := p.GetUsage()
	if err != nil {
		t.Fatalf("probe should succeed for valid key: %v", err)
	}
	if usage.Rolling.Status != StatusUnknown {
		t.Errorf("expected unknown rolling window without headers, got %q", usage.Rolling.Status)
	}
	if !strings.Contains(usage.Note, "arkcli") {
		t.Errorf("expected note to mention arkcli, got %q", usage.Note)
	}
}

func TestVolcengineProbe_WithRateLimitHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Ratelimit-Limit-Requests", "100")
		w.Header().Set("X-Ratelimit-Remaining-Requests", "55")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	p := &VolcengineProvider{apiKey: "k", plan: PlanCoding, probeBase: server.URL}
	usage, err := p.GetUsage()
	if err != nil {
		t.Fatalf("probe failed: %v", err)
	}
	if usage.Rolling.Percent != 45 {
		t.Errorf("expected rolling percent 45 (100-55), got %d", usage.Rolling.Percent)
	}
}

func TestVolcengineProbe_InvalidKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	p := &VolcengineProvider{apiKey: "bad", plan: PlanCoding, probeBase: server.URL}
	if _, err := p.GetUsage(); err == nil {
		t.Fatal("expected error for rejected key")
	}
}

func TestVolcengineIsAvailable(t *testing.T) {
	if (&VolcengineProvider{}).IsAvailable() {
		t.Error("expected unavailable with no key and no arkcli")
	}
	if !(&VolcengineProvider{apiKey: "k"}).IsAvailable() {
		t.Error("expected available with key")
	}
	if !(&VolcengineProvider{arkcli: "/usr/bin/arkcli"}).IsAvailable() {
		t.Error("expected available with arkcli path")
	}
}

func TestVolcengineArkcliParsing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake arkcli requires a POSIX shell; arkcli parsing is covered by the JSON structure on Windows")
	}
	script := `#!/bin/sh
echo '{"items":[{"product":"coding-plan","edition":"personal","subscribed":true,"periods":[{"label":"session","percent":42.5,"reset_at":"2026-09-05T20:00:00+08:00"},{"label":"weekly","percent":10.42,"reset_at":"2026-09-08T00:00:00+08:00"},{"label":"monthly","percent":5.21,"reset_at":"2026-09-29T00:00:00+08:00"}]},{"product":"agent-plan","subscribed":false,"periods":[]}]}'
`
	dir := t.TempDir()
	bin := filepath.Join(dir, "arkcli")
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write fake arkcli: %v", err)
	}

	p := &VolcengineProvider{plan: PlanCoding, arkcli: bin}
	usage, err := p.GetUsage()
	if err != nil {
		t.Fatalf("arkcli path failed: %v", err)
	}
	if usage.PlanType != "coding-plan (personal)" {
		t.Errorf("unexpected plan type %q", usage.PlanType)
	}
	if usage.Rolling.Percent != 43 || usage.Weekly.Percent != 10 || usage.Monthly.Percent != 5 {
		t.Errorf("unexpected windows: %+v", usage)
	}
}

func TestArkcliOutputParsing(t *testing.T) {
	// Platform-independent: parse the arkcli JSON shape directly.
	out := arkcliOutput{
		Items: []arkcliItem{
			{Product: "coding-plan", Edition: "team", Subscribed: true, Periods: []arkcliPeriod{
				{Label: "session", Percent: 7},
			}},
			{Product: "coding-plan", Edition: "personal", Subscribed: true, Periods: []arkcliPeriod{
				{Label: "session", Percent: 42},
				{Label: "weekly", Percent: 10},
			}},
			{Product: "agent-plan", Subscribed: false},
		},
	}
	best := pickArkcliItem(out, "coding-plan")
	if best == nil || best.Edition != "personal" {
		t.Fatalf("expected personal coding-plan item to win, got %+v", best)
	}
	if pickArkcliItem(out, "agent-plan") != nil {
		t.Error("unsubscribed agent-plan must be skipped")
	}
}

func TestVolcengineArkcliProfileFlag(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake arkcli requires a POSIX shell")
	}
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	script := `#!/bin/sh
printf '%s\n' "$@" >> "$TU_TEST_ARGS_FILE"
echo '{"items":[{"product":"coding-plan","subscribed":true,"periods":[{"label":"session","percent":1}]}]}'
`
	bin := filepath.Join(dir, "arkcli")
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write fake arkcli: %v", err)
	}
	t.Setenv("TU_TEST_ARGS_FILE", argsFile)

	p := &VolcengineProvider{plan: PlanCoding, arkcli: bin, profile: "p2"}
	if _, err := p.GetUsage(); err != nil {
		t.Fatalf("arkcli path failed: %v", err)
	}
	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("fake arkcli did not record args: %v", err)
	}
	got := strings.Join(strings.Fields(string(data)), " ")
	if !strings.Contains(got, "--profile p2 usage plan --format json") {
		t.Errorf("expected usage plan args with profile, got %q", got)
	}
}

func TestVolcengineArkcliNoProfileOmitsFlag(t *testing.T) {
	args := (&VolcengineProvider{plan: PlanCoding}).arkcliArgs("")
	got := strings.Join(args, " ")
	want := "usage plan --format json"
	if got != want {
		t.Errorf("arkcli args = %q, want %q", got, want)
	}
}

func TestVolcengineArkcliHomeEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake arkcli requires a POSIX shell")
	}
	dir := t.TempDir()
	altHome := filepath.Join(dir, "alt-home")
	if err := os.MkdirAll(altHome, 0755); err != nil {
		t.Fatal(err)
	}
	argsFile := filepath.Join(dir, "env.txt")
	script := `#!/bin/sh
echo "$HOME" > "$TU_TEST_ARGS_FILE"
echo '{"items":[{"product":"coding-plan","subscribed":true,"periods":[{"label":"session","percent":1}]}]}'
`
	bin := filepath.Join(dir, "arkcli")
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write fake arkcli: %v", err)
	}
	t.Setenv("TU_TEST_ARGS_FILE", argsFile)

	p := &VolcengineProvider{plan: PlanCoding, arkcli: bin, arkcliHome: altHome}
	if _, err := p.GetUsage(); err != nil {
		t.Fatalf("arkcli path failed: %v", err)
	}
	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("fake arkcli did not record HOME: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != altHome {
		t.Errorf("arkcli ran with HOME=%q, want %q", got, altHome)
	}
}

func TestArkcliProfilesParsing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake arkcli requires a POSIX shell")
	}
	script := `#!/bin/sh
echo '{"default_profile":"p1","profiles":[{"name":"p1","display_name":"Plan A","type":"coding-plan","plan_tier":"pro","region":"cn-beijing","is_default":true,"default_api_key":"ark****fa7d"},{"name":"p2","type":"platform","default_api_key":"ark****1d5"}]}'
`
	dir := t.TempDir()
	bin := filepath.Join(dir, "arkcli")
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write fake arkcli: %v", err)
	}

	profiles, err := arkcliProfiles(bin, "")
	if err != nil {
		t.Fatalf("arkcliProfiles failed: %v", err)
	}
	if len(profiles) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(profiles))
	}
	if profiles[0].Name != "p1" || profiles[0].DisplayName != "Plan A" || !profiles[0].IsDefault {
		t.Errorf("unexpected first profile: %+v", profiles[0])
	}
	if profiles[1].Name != "p2" || profiles[1].IsDefault {
		t.Errorf("unexpected second profile: %+v", profiles[1])
	}

	if _, err := arkcliProfiles("", ""); err == nil {
		t.Error("expected error when arkcli is missing")
	}
}

func TestMaskedKeySuffix(t *testing.T) {
	cases := []struct {
		masked, want string
	}{
		{"ark****fa7d", "fa7d"},
		{"ark-****f1d5-6", "f1d5-6"},
		{"nomask", ""},
		{"trail*", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := maskedKeySuffix(c.masked); got != c.want {
			t.Errorf("maskedKeySuffix(%q) = %q, want %q", c.masked, got, c.want)
		}
	}
}

func TestMatchProfile(t *testing.T) {
	profiles := []ArkcliProfile{
		{Name: "p1", IsDefault: true, DefaultAPIKey: "ark****fa7d"},
		{Name: "p2", DefaultAPIKey: "ark****1d5"},
		{Name: "p3"}, // no key listed: never matches
	}
	if got := matchProfile(profiles, "ark-36a9-75db-4016-936d-514dd57eb4d5-6fa7d"); got != "p1" {
		t.Errorf("matching key = %q, want p1", got)
	}
	if got := matchProfile(profiles, "ark-d743-xxxxxx-f1d5"); got != "p2" {
		t.Errorf("second key = %q, want p2", got)
	}
	if got := matchProfile(profiles, "ark-0000-zzzzzz-0000"); got != "" {
		t.Errorf("unmatched key = %q, want empty", got)
	}
}

func TestVolcengineGetUsage_AutoMatchUsesArkcli(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake arkcli requires a POSIX shell")
	}
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	script := `#!/bin/sh
printf '%s\n' "$@" >> "$TU_TEST_ARGS_FILE"
echo '{"items":[{"product":"coding-plan","subscribed":true,"periods":[{"label":"session","percent":9}]}]}'
`
	bin := filepath.Join(dir, "arkcli")
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write fake arkcli: %v", err)
	}
	t.Setenv("TU_TEST_ARGS_FILE", argsFile)
	old := matchProfileForKey
	matchProfileForKey = func(string, string) string { return "p1" }
	defer func() { matchProfileForKey = old }()

	p := &VolcengineProvider{apiKey: "ark-k", plan: PlanCoding, arkcli: bin}
	usage, err := p.GetUsage()
	if err != nil {
		t.Fatalf("GetUsage failed: %v", err)
	}
	if usage.Rolling.Percent != 9 {
		t.Errorf("expected arkcli-provided windows, got %+v", usage)
	}
	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("arkcli was not called: %v", err)
	}
	got := strings.Join(strings.Fields(string(data)), " ")
	if !strings.Contains(got, "--profile p1 usage plan") {
		t.Errorf("expected --profile p1 in args, got %q", got)
	}
}

func TestVolcengineGetUsage_NoMatchFallsBackToProbe(t *testing.T) {
	probeHit := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probeHit = true
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"choices": []map[string]interface{}{}})
	}))
	defer server.Close()

	old := matchProfileForKey
	matchProfileForKey = func(string, string) string { return "" } // key belongs to no logged-in profile
	defer func() { matchProfileForKey = old }()

	p := &VolcengineProvider{apiKey: "ark-other-account", plan: PlanCoding, arkcli: "/nonexistent/arkcli", probeBase: server.URL}
	usage, err := p.GetUsage()
	if err != nil {
		t.Fatalf("probe path failed: %v", err)
	}
	if !probeHit {
		t.Error("expected the probe endpoint to be called when no profile matches")
	}
	if usage.Rolling.Status != StatusUnknown {
		t.Errorf("expected unknown windows from probe, got %+v", usage.Rolling)
	}
}

// arkcliUsagePlanSsoErr is arkcli's verbatim failure when the server rejects
// the HOME's stored SSO refresh token (printed to stderr, exit 1).
const arkcliUsagePlanSsoErr = `{
  "ok": false,
  "error": {
    "type": "error",
    "message": "auto-discover subscriptions: trade.list_subscribe: ark: ListSubscribeTrade: ark: ListSubscribeTrade requires Volcengine Ark SSO STS, please run ` + "`arkcli auth login volc-sso`" + `: identity volc-2130704960 STS 续期失败: token 交换失败: invalid_request - The request parameter refresh_token is invalid. "
  }
}`

func TestArkcliSsoExpiredErr(t *testing.T) {
	// Mirror the real path: arkcli's JSON arrives on stderr, gets collapsed
	// by collapseMsg and wrapped by usage_plan_failed, unclipped.
	wrapped := i18n.T("provider.volcengine.usage_plan_failed", collapseMsg(arkcliUsagePlanSsoErr))
	if !arkcliSsoExpiredErr(errors.New(wrapped)) {
		t.Errorf("real arkcli SSO-expiry error must be detected, got %q", wrapped)
	}
	for _, msg := range []string{
		"usage plan failed: exit status 1",
		"no active coding-plan subscription found",
		"",
	} {
		if arkcliSsoExpiredErr(errors.New(msg)) {
			t.Errorf("unrelated error %q must not be flagged", msg)
		}
	}
	if arkcliSsoExpiredErr(nil) {
		t.Error("nil must not be flagged")
	}
}

func TestVolcengineGetUsage_SsoExpiredShowsReloginNote(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake arkcli requires a POSIX shell")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"choices": []map[string]interface{}{}})
	}))
	defer server.Close()

	dir := t.TempDir()
	bin := filepath.Join(dir, "arkcli")
	script := `#!/bin/sh
cat >&2 <<'EOF'
` + arkcliUsagePlanSsoErr + `
EOF
exit 1
`
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write fake arkcli: %v", err)
	}

	altHome := filepath.Join(dir, "home")
	if err := os.MkdirAll(altHome, 0755); err != nil {
		t.Fatal(err)
	}
	p := &VolcengineProvider{
		apiKey:     "ark-k-f1d5",
		plan:       PlanCoding,
		profile:    "p2", // skip auto-match; go straight to usage plan
		arkcliHome: altHome,
		arkcli:     bin,
		probeBase:  server.URL,
	}
	usage, err := p.GetUsage()
	if err != nil {
		t.Fatalf("probe fallback should succeed: %v", err)
	}
	if !strings.Contains(usage.Note, "volc-sso login expired") {
		t.Errorf("expected dedicated SSO-expired note, got %q", usage.Note)
	}
	if !strings.Contains(usage.Note, "HOME="+altHome+" arkcli auth login volc-sso") {
		t.Errorf("expected re-login command with the account HOME, got %q", usage.Note)
	}
	if strings.Contains(usage.Note, `"ok"`) {
		t.Errorf("raw arkcli JSON must not leak into the note, got %q", usage.Note)
	}
}

func TestVolcengineGetUsage_SsoExpiredStdoutOnly(t *testing.T) {
	// arkcli may print its error JSON to stdout instead of stderr; the
	// capture fallback must still surface the SSO-expiry markers.
	if runtime.GOOS == "windows" {
		t.Skip("fake arkcli requires a POSIX shell")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"choices": []map[string]interface{}{}})
	}))
	defer server.Close()

	dir := t.TempDir()
	bin := filepath.Join(dir, "arkcli")
	script := `#!/bin/sh
cat <<'EOF'
` + arkcliUsagePlanSsoErr + `
EOF
exit 1
`
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write fake arkcli: %v", err)
	}

	p := &VolcengineProvider{apiKey: "ark-k", plan: PlanCoding, profile: "p1", arkcli: bin, probeBase: server.URL}
	usage, err := p.GetUsage()
	if err != nil {
		t.Fatalf("probe fallback should succeed: %v", err)
	}
	if !strings.Contains(usage.Note, "volc-sso login expired") {
		t.Errorf("expected dedicated SSO-expired note from stdout-captured error, got %q", usage.Note)
	}
}

func TestVolcengineGetUsage_GenericArkcliFailureKeepsTruncatedNote(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake arkcli requires a POSIX shell")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"choices": []map[string]interface{}{}})
	}))
	defer server.Close()

	dir := t.TempDir()
	bin := filepath.Join(dir, "arkcli")
	script := `#!/bin/sh
echo 'usage plan failed' >&2
echo '{"unexpected":"stdout"}'
exit 1
`
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write fake arkcli: %v", err)
	}

	p := &VolcengineProvider{apiKey: "ark-k", plan: PlanCoding, profile: "p1", arkcli: bin, probeBase: server.URL}
	usage, err := p.GetUsage()
	if err != nil {
		t.Fatalf("probe fallback should succeed: %v", err)
	}
	if !strings.Contains(usage.Note, "arkcli 查询失败") && !strings.Contains(usage.Note, "arkcli query failed") {
		t.Errorf("expected generic arkcli-failed note, got %q", usage.Note)
	}
	if strings.Contains(usage.Note, "stdout") || strings.Contains(usage.Note, "\n") {
		t.Errorf("expected collapsed single-line message without stdout noise, got %q", usage.Note)
	}
}

func TestArkcliHomeStatus(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake arkcli requires a POSIX shell")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "arkcli")
	script := `#!/bin/sh
echo '{"logged_in":true,"sso_expired":true,"account_id":"2130704960"}'
`
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write fake arkcli: %v", err)
	}
	// Point PATH at the fake binary so lookPathArkcli resolves it.
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	st, err := ArkcliHomeLoginState("", "")
	if err != nil {
		t.Fatalf("ArkcliHomeLoginState failed: %v", err)
	}
	if !st.LoggedIn || !st.SsoExpired {
		t.Errorf("unexpected status: %+v", st)
	}
}
