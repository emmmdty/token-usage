package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/emmmdty/token-usage/internal/i18n"
)

type OpenCodeProvider struct {
	apiKey   string
	endpoint string
}

func NewOpenCodeProvider(apiKey string) *OpenCodeProvider {
	return &OpenCodeProvider{
		apiKey:   apiKey,
		endpoint: "https://opencode.ai/zen/go/v1",
	}
}

func NewOpenCodeProviderWithEndpoint(apiKey, endpoint string) *OpenCodeProvider {
	p := NewOpenCodeProvider(apiKey)
	if endpoint != "" {
		p.endpoint = endpoint
	}
	return p
}

func (p *OpenCodeProvider) Name() string {
	return "opencode"
}

// normalizeWindowStatus maps the API's window status onto the shared
// vocabulary. Verified against the live endpoint (2026-09-16): a used-up
// window reports status "rate-limited" with percent 100 and a resetsAt, so
// it becomes the canonical exhausted status instead of n/a. Recognized
// pass-throughs stay as-is; anything unrecognized falls back to unknown so
// a future status can never masquerade as healthy.
func normalizeWindowStatus(s string) string {
	switch s {
	case "ok", "idle", StatusExhausted, StatusNone:
		return s
	case "rate-limited":
		return StatusExhausted
	default:
		return StatusUnknown
	}
}

func (p *OpenCodeProvider) IsAvailable() bool {
	return p.apiKey != ""
}

func (p *OpenCodeProvider) GetUsage() (*Usage, error) {
	req, err := http.NewRequest("GET", p.endpoint+"/usage", nil)
	if err != nil {
		return nil, fmt.Errorf("%s", i18n.T("provider.opencode.create_request", err))
	}

	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s", i18n.T("provider.opencode.make_request", err))
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s", i18n.T("provider.opencode.api_error", resp.StatusCode))
	}

	var result struct {
		Usage struct {
			Rolling struct {
				Status   string `json:"status"`
				Percent  int    `json:"percent"`
				ResetsAt string `json:"resetsAt"`
			} `json:"rolling"`
			Weekly struct {
				Status   string `json:"status"`
				Percent  int    `json:"percent"`
				ResetsAt string `json:"resetsAt"`
			} `json:"weekly"`
			Monthly struct {
				Status   string `json:"status"`
				Percent  int    `json:"percent"`
				ResetsAt string `json:"resetsAt"`
			} `json:"monthly"`
		} `json:"usage"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	usage := &Usage{
		Provider: "opencode",
		PlanType: "subscription",
	}

	// Parse rolling
	resetAt, _ := time.Parse(time.RFC3339, result.Usage.Rolling.ResetsAt)
	usage.Rolling = QuotaWindow{
		Status:  normalizeWindowStatus(result.Usage.Rolling.Status),
		Percent: result.Usage.Rolling.Percent,
		ResetAt: resetAt,
	}

	// Parse weekly
	resetAt, _ = time.Parse(time.RFC3339, result.Usage.Weekly.ResetsAt)
	usage.Weekly = QuotaWindow{
		Status:  normalizeWindowStatus(result.Usage.Weekly.Status),
		Percent: result.Usage.Weekly.Percent,
		ResetAt: resetAt,
	}

	// Parse monthly
	resetAt, _ = time.Parse(time.RFC3339, result.Usage.Monthly.ResetsAt)
	usage.Monthly = QuotaWindow{
		Status:  normalizeWindowStatus(result.Usage.Monthly.Status),
		Percent: result.Usage.Monthly.Percent,
		ResetAt: resetAt,
	}

	return usage, nil
}
