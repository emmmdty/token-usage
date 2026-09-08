package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"errors"
	"github.com/emmmdty/token-usage/internal/i18n"
)

// KeyQuery fetches usage with an API key and an optional base URL. Every
// key-based provider (presets and custom entries) resolves to one of these.
type KeyQuery func(apiKey, baseURL string) (*Usage, error)

// BuiltinKeyQueries lists the registry keys available for custom providers
// (display order matters for the interactive menu).
var BuiltinKeyQueries = []string{"zai-glm", "kimi", "minimax", "deepseek", "openai-compatible"}

// LookupKeyQuery resolves a query type from the registry.
func LookupKeyQuery(queryType string) (KeyQuery, bool) {
	q, ok := keyQueries[queryType]
	return q, ok
}

var keyQueries = map[string]KeyQuery{
	"opencode":          opencodeKeyQuery,
	"zai-glm":           zaiGLMQuery,
	"kimi":              kimiQuery,
	"minimax":           minimaxQuery,
	"deepseek":          deepseekQuery,
	"openai-compatible": openAICompatibleQuery,
}

func newHTTP(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout}
}

func getJSON(url, authHeader string, out interface{}) (int, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, err
	}
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	resp, err := newHTTP(15 * time.Second).Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, err
	}
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, fmt.Errorf("%s", i18n.T("provider.vendors.http_error", resp.StatusCode, truncateMsg(strings.TrimSpace(string(body)), 160)))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return resp.StatusCode, fmt.Errorf("%s", i18n.T("provider.vendors.unexpected_format", err))
	}
	return resp.StatusCode, nil
}

// opencodeKeyQuery adapts the existing OpenCode Go client to the registry.
func opencodeKeyQuery(apiKey, baseURL string) (*Usage, error) {
	return NewOpenCodeProviderWithEndpoint(apiKey, baseURL).GetUsage()
}

// --- Z.ai / Zhipu GLM Coding Plan -------------------------------------------
// GET /api/monitor/usage/quota/limit
// Auth: "Bearer <key>" on api.z.ai; open.bigmodel.cn also accepts the bare
// token. We try Bearer first and retry bare on 401.
// Response: {"code":200,"success":true,"data":{"limits":[{"type":"TOKENS_LIMIT"|
// "CREDIT_LIMIT"|"TIME_LIMIT","percentage":42,...}]}}
func zaiGLMQuery(apiKey, baseURL string) (*Usage, error) {
	base := strings.TrimSuffix(baseURL, "/")
	if base == "" {
		base = "https://api.z.ai"
	}
	endpoint := base + "/api/monitor/usage/quota/limit"

	type zaiLimit struct {
		Type       string          `json:"type"`
		Percentage json.RawMessage `json:"percentage"`
	}
	type zaiResp struct {
		Code    int  `json:"code"`
		Success bool `json:"success"`
		Data    struct {
			Limits []zaiLimit `json:"limits"`
		} `json:"data"`
	}

	var out zaiResp
	status, err := getJSON(endpoint, "Bearer "+apiKey, &out)
	if err != nil && status == http.StatusUnauthorized {
		// Retry with the bare token (open.bigmodel.cn style). The retry's
		// status is unused, so only the error matters here.
		_, err = getJSON(endpoint, apiKey, &out)
	}
	if err != nil {
		return nil, err
	}
	if out.Code != 200 || !out.Success {
		return nil, fmt.Errorf("%s", i18n.T("provider.vendors.zai_glm.rejected", out.Code))
	}

	usage := &Usage{
		Provider: "zai-glm",
		PlanType: "coding-plan",
		Rolling:  QuotaWindow{Status: StatusUnknown},
		Weekly:   QuotaWindow{Status: StatusUnknown},
		Monthly:  QuotaWindow{Status: StatusUnknown},
	}
	for _, limit := range out.Data.Limits {
		pct, ok := rawToPercent(limit.Percentage)
		if !ok {
			// A malformed percentage must never render as a healthy 0%;
			// leave the window unknown (or fail when nothing is usable).
			continue
		}
		window := QuotaWindow{Status: "ok", Percent: pct}
		switch limit.Type {
		case "TOKENS_LIMIT":
			usage.Rolling = window
		case "CREDIT_LIMIT":
			usage.Monthly = window
		case "TIME_LIMIT":
			// Time windows carry no usage percentage; skip.
		}
	}
	if usage.Rolling.Status == StatusUnknown && usage.Monthly.Status == StatusUnknown {
		return nil, errors.New(i18n.T("provider.vendors.zai_glm.no_limits"))
	}
	return usage, nil
}

// clampPercent confines an unvalidated provider percentage to 0..100.
func clampPercent(p int) int {
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}

func rawToPercent(raw json.RawMessage) (int, bool) {
	s := strings.Trim(string(raw), `"`)
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return int(f), true
	}
	return 0, false
}

// --- Kimi (Moonshot) Coding Plan ---------------------------------------------
// GET https://api.kimi.com/coding/v1/usages
// Bearer. Response: {"usage":{"limit":N,"remaining":N,"used":N,"resetTime":...},
// "limits":[{"window":{"duration":300,"timeUnit":"MINUTE"},"detail":{...}}]}
func kimiQuery(apiKey, baseURL string) (*Usage, error) {
	base := strings.TrimSuffix(baseURL, "/")
	if base == "" {
		base = "https://api.kimi.com"
	}

	type kimiDetail struct {
		Limit     *float64 `json:"limit"`
		Remaining *float64 `json:"remaining"`
		Used      *float64 `json:"used"`
		ResetTime string   `json:"resetTime"`
	}
	type kimiWindow struct {
		Window struct {
			Duration *int   `json:"duration"`
			TimeUnit string `json:"timeUnit"`
		} `json:"window"`
		Detail kimiDetail `json:"detail"`
	}
	type kimiResp struct {
		Usage  kimiDetail   `json:"usage"`
		Limits []kimiWindow `json:"limits"`
	}

	var out kimiResp
	if _, err := getJSON(base+"/coding/v1/usages", "Bearer "+apiKey, &out); err != nil {
		return nil, err
	}

	usage := &Usage{
		Provider: "kimi",
		PlanType: "coding-plan",
		Rolling:  QuotaWindow{Status: StatusUnknown},
		Weekly:   QuotaWindow{Status: StatusUnknown},
		Monthly:  QuotaWindow{Status: StatusUnknown},
	}

	// Weekly window comes from the top-level usage block.
	if w := windowFromDetail(out.Usage); w != nil {
		usage.Weekly = *w
	}
	// Short rate-limit windows: 300 minutes == 5h rolling window.
	for _, l := range out.Limits {
		if l.Window.Duration != nil && *l.Window.Duration == 300 && strings.EqualFold(l.Window.TimeUnit, "MINUTE") {
			if w := windowFromDetail(l.Detail); w != nil {
				usage.Rolling = *w
			}
		}
	}
	if usage.Rolling.Status == StatusUnknown && usage.Weekly.Status == StatusUnknown {
		return nil, errors.New(i18n.T("provider.vendors.kimi.no_windows"))
	}
	return usage, nil
}

func windowFromDetail(d struct {
	Limit     *float64 `json:"limit"`
	Remaining *float64 `json:"remaining"`
	Used      *float64 `json:"used"`
	ResetTime string   `json:"resetTime"`
}) *QuotaWindow {
	if d.Limit == nil && d.Remaining == nil && d.Used == nil {
		return nil
	}
	used := 0.0
	switch {
	case d.Used != nil:
		used = *d.Used
	case d.Limit != nil && d.Remaining != nil:
		used = *d.Limit - *d.Remaining
	default:
		return nil
	}
	w := &QuotaWindow{Status: "ok"}
	if d.Limit != nil && *d.Limit > 0 {
		w.Percent = int(used / *d.Limit * 100)
	}
	if t, err := time.Parse(time.RFC3339, d.ResetTime); err == nil {
		w.ResetAt = t
	}
	return w
}

// --- MiniMax Coding Plan ------------------------------------------------------
// GET https://api.minimax.io/v1/api/openplatform/coding_plan/remains
// (CN host: api.minimaxi.com — override via base URL)
// Bearer. Response: {"base_resp":{"status_code":0},
// "model_remains":[{"model_name":..,"total":..,"remaining":..,
// "remaining_percent":..,"end_time":..}], "current_subscribe_title":..}
func minimaxQuery(apiKey, baseURL string) (*Usage, error) {
	base := strings.TrimSuffix(baseURL, "/")
	if base == "" {
		base = "https://api.minimax.io"
	}

	type miniRemain struct {
		ModelName        string   `json:"model_name"`
		Total            *float64 `json:"total"`
		Remaining        *float64 `json:"remaining"`
		RemainingPercent *float64 `json:"remaining_percent"`
		Status           *int     `json:"status"`
		EndTime          *int64   `json:"end_time"`
	}
	type miniResp struct {
		BaseResp struct {
			StatusCode *int `json:"status_code"`
		} `json:"base_resp"`
		ModelRemains          []miniRemain `json:"model_remains"`
		CurrentSubscribeTitle string       `json:"current_subscribe_title"`
	}

	var out miniResp
	if _, err := getJSON(base+"/v1/api/openplatform/coding_plan/remains", "Bearer "+apiKey, &out); err != nil {
		return nil, err
	}
	if out.BaseResp.StatusCode != nil && *out.BaseResp.StatusCode != 0 {
		return nil, fmt.Errorf("%s", i18n.T("provider.vendors.minimax.rejected", *out.BaseResp.StatusCode))
	}
	if len(out.ModelRemains) == 0 {
		return nil, errors.New(i18n.T("provider.vendors.minimax.no_entries"))
	}

	usage := &Usage{
		Provider: "minimax",
		PlanType: "coding-plan",
		Rolling:  QuotaWindow{Status: StatusUnknown},
		Weekly:   QuotaWindow{Status: StatusUnknown},
		Monthly:  QuotaWindow{Status: StatusUnknown},
	}
	if out.CurrentSubscribeTitle != "" {
		usage.PlanType = "coding-plan (" + out.CurrentSubscribeTitle + ")"
	}

	// Multiple model buckets: the tightest one drives the headline percent;
	// every bucket lands in RawData for the JSON view.
	worst := 0
	for _, m := range out.ModelRemains {
		pct := 0
		switch {
		case m.RemainingPercent != nil:
			pct = clampPercent(100 - int(*m.RemainingPercent))
		case m.Total != nil && *m.Total > 0 && m.Remaining != nil:
			pct = clampPercent(int((*m.Total - *m.Remaining) / *m.Total * 100))
		}
		if pct > worst {
			worst = pct
		}
	}
	usage.Rolling = QuotaWindow{Status: "ok", Percent: worst}
	for _, m := range out.ModelRemains {
		if m.EndTime != nil && *m.EndTime > 0 && usage.Rolling.ResetAt.IsZero() {
			usage.Rolling.ResetAt = time.Unix(*m.EndTime, 0)
			break
		}
	}
	usage.RawData = out.ModelRemains
	return usage, nil
}

// --- DeepSeek (pay-as-you-go balance) ------------------------------------------
// GET https://api.deepseek.com/user/balance
// Bearer. Response: {"is_available":true,"balance_infos":[{"currency":"CNY",
// "total_balance":"12.34",...}]}
func deepseekQuery(apiKey, baseURL string) (*Usage, error) {
	base := strings.TrimSuffix(baseURL, "/")
	if base == "" {
		base = "https://api.deepseek.com"
	}

	type dsResp struct {
		IsAvailable  bool `json:"is_available"`
		BalanceInfos []struct {
			Currency       string `json:"currency"`
			TotalBalance   string `json:"total_balance"`
			GrantedBalance string `json:"granted_balance"`
		} `json:"balance_infos"`
	}

	var out dsResp
	if _, err := getJSON(base+"/user/balance", "Bearer "+apiKey, &out); err != nil {
		return nil, err
	}
	if len(out.BalanceInfos) == 0 {
		return nil, errors.New(i18n.T("provider.vendors.deepseek.no_balance"))
	}
	b := out.BalanceInfos[0]
	if !out.IsAvailable {
		return nil, fmt.Errorf("%s", i18n.T("provider.vendors.deepseek.not_available", b.TotalBalance, b.Currency))
	}

	return &Usage{
		Provider: "deepseek",
		PlanType: "pay-as-you-go",
		Rolling:  QuotaWindow{Status: StatusUnknown},
		Weekly:   QuotaWindow{Status: StatusUnknown},
		Monthly:  QuotaWindow{Status: StatusUnknown},
		Note:     fmt.Sprintf(i18n.T("provider.vendors.deepseek.note"), b.TotalBalance, b.Currency),
	}, nil
}

// --- Generic OpenAI-compatible --------------------------------------------------
// No standard quota endpoint exists. Probe /models for key validity and try
// the OpenAI-style billing endpoints; without a usable quota source the query
// fails loudly so custom-provider validation rejects the entry.
func openAICompatibleQuery(apiKey, baseURL string) (*Usage, error) {
	base := strings.TrimSuffix(baseURL, "/")
	if base == "" {
		return nil, errors.New(i18n.T("provider.vendors.openai.base_url_required"))
	}

	type billingSub struct {
		TotalGranted float64 `json:"total_granted"`
		TotalUsed    float64 `json:"total_used"`
		TotalAvail   float64 `json:"total_available"`
	}
	var sub billingSub
	if _, err := getJSON(base+"/dashboard/billing/subscription", "Bearer "+apiKey, &sub); err == nil && sub.TotalGranted > 0 {
		pct := 0
		if sub.TotalGranted > 0 {
			pct = int(sub.TotalUsed / sub.TotalGranted * 100)
		}
		return &Usage{
			Provider: "custom",
			PlanType: "prepaid",
			Rolling:  QuotaWindow{Status: "ok", Percent: pct},
			Weekly:   QuotaWindow{Status: StatusUnknown},
			Monthly:  QuotaWindow{Status: StatusUnknown},
			Note:     fmt.Sprintf(i18n.T("provider.vendors.openai.note"), sub.TotalUsed, sub.TotalGranted),
		}, nil
	}

	return nil, fmt.Errorf("%s", i18n.T("provider.vendors.openai.no_endpoint", base))
}
