package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"errors"
	"github.com/emmmdty/token-usage/internal/i18n"
)

type CodexProvider struct {
	authPath string
	endpoint string
	// accessToken, when set, is used directly instead of reading the local
	// auth file (manual accounts).
	accessToken string
}

// codexClientUA mirrors the official Codex CLI user agent. Empirically
// (verified 2026-09-06 against the live endpoint) wham/usage does NOT gate
// on User-Agent — empty and curl UAs return 200 with a valid Bearer token,
// 401 without — so this header is defensive camouflage, not a functional
// requirement; it is kept so requests blend in with official client traffic
// in case bot protection tightens. Bump it when the installed codex CLI
// moves significantly ahead (the test pins this value).
const codexClientUA = "codex-cli/0.153.4"

func NewCodexProvider(authPath string) *CodexProvider {
	return &CodexProvider{
		authPath: authPath,
		endpoint: "https://chatgpt.com",
	}
}

func NewCodexProviderWithEndpoint(authPath, endpoint string) *CodexProvider {
	p := NewCodexProvider(authPath)
	if endpoint != "" {
		p.endpoint = endpoint
	}
	return p
}

// NewCodexProviderWithToken builds a provider that uses a pasted OAuth
// access token instead of the local auth file.
func NewCodexProviderWithToken(accessToken, endpoint string) *CodexProvider {
	p := NewCodexProvider("")
	p.accessToken = accessToken
	if endpoint != "" {
		p.endpoint = endpoint
	}
	return p
}

func (p *CodexProvider) Name() string {
	return "codex"
}

func (p *CodexProvider) IsAvailable() bool {
	if p.accessToken != "" {
		return true
	}
	_, err := os.Stat(p.authPath)
	return err == nil
}

type codexAuth struct {
	Tokens struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	} `json:"tokens"`
}

func (p *CodexProvider) loadAuth() (*codexAuth, error) {
	if p.accessToken != "" {
		return &codexAuth{}, nil
	}
	data, err := os.ReadFile(p.authPath)
	if err != nil {
		return nil, fmt.Errorf("%s", i18n.T("provider.codex.read_auth", err))
	}

	var auth codexAuth
	if err := json.Unmarshal(data, &auth); err != nil {
		return nil, fmt.Errorf("%s", i18n.T("provider.codex.parse_auth", err))
	}

	if auth.Tokens.AccessToken == "" {
		return nil, errors.New(i18n.T("provider.codex.no_token"))
	}

	return &auth, nil
}

func (p *CodexProvider) GetUsage() (*Usage, error) {
	auth, err := p.loadAuth()
	if err != nil {
		return nil, err
	}

	// 使用 wham/usage 端点（codex/usage 有 Cloudflare 保护）
	req, err := http.NewRequest("GET", p.endpoint+"/backend-api/wham/usage", nil)
	if err != nil {
		return nil, fmt.Errorf("%s", i18n.T("provider.codex.create_request", err))
	}

	token := p.accessToken
	if token == "" {
		token = auth.Tokens.AccessToken
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", codexClientUA)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s", i18n.T("provider.codex.make_request", err))
	}
	defer func() { _ = resp.Body.Close() }()

	// 读取响应体
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%s", i18n.T("provider.codex.read_response", err))
	}
	respStr := string(respBody)

	// 检查是否返回 HTML（通常是认证失败）
	if strings.Contains(respStr, "<html>") || strings.Contains(respStr, "<!DOCTYPE") {
		return nil, errors.New(i18n.T("provider.codex.token_expired"))
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s", i18n.T("provider.codex.api_error", resp.StatusCode, truncateStr(respStr, 100)))
	}

	var result struct {
		PlanType  string `json:"plan_type"`
		RateLimit struct {
			PrimaryWindow struct {
				UsedPercent    float64 `json:"used_percent"`
				ResetAfterSecs int     `json:"reset_after_seconds"`
				ResetAt        *int64  `json:"reset_at"`
			} `json:"primary_window"`
			SecondaryWindow struct {
				UsedPercent    float64 `json:"used_percent"`
				ResetAfterSecs int     `json:"reset_after_seconds"`
				ResetAt        *int64  `json:"reset_at"`
			} `json:"secondary_window"`
		} `json:"rate_limit"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("%s", i18n.T("provider.codex.decode_response", err))
	}

	// A window without reset_at resolves to a zero time (rendered "n/a")
	// instead of 1970-01-01 (rendered "expired").
	resetAtOrZero := func(p *int64) time.Time {
		if p == nil {
			return time.Time{}
		}
		return time.Unix(*p, 0)
	}

	usage := &Usage{
		Provider: "codex",
		PlanType: result.PlanType,
	}

	// Parse 5h window (primary)
	usage.Rolling = QuotaWindow{
		Percent: int(result.RateLimit.PrimaryWindow.UsedPercent),
		ResetAt: resetAtOrZero(result.RateLimit.PrimaryWindow.ResetAt),
		Status:  "ok",
	}

	// Parse 7d window (secondary)
	usage.Weekly = QuotaWindow{
		Percent: int(result.RateLimit.SecondaryWindow.UsedPercent),
		ResetAt: resetAtOrZero(result.RateLimit.SecondaryWindow.ResetAt),
		Status:  "ok",
	}

	return usage, nil
}

func (p *CodexProvider) GetDefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex", "auth.json")
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
