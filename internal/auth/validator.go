package auth

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/emmmdty/token-usage/internal/i18n"
)

const (
	defaultBaseURL = "https://opencode.ai/zen/go/v1"
	timeout        = 10 * time.Second
)

type ValidationResponse struct {
	Valid   bool
	Error   string
	Message string
}

func ValidateAPIKey(apiKey, baseURL string) (*ValidationResponse, error) {
	if baseURL == "" {
		baseURL = os.Getenv("TOKEN_USAGE_BASE_URL")
		if baseURL == "" {
			baseURL = defaultBaseURL
		}
	}

	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest("GET", baseURL+"/usage", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return &ValidationResponse{
			Valid:   false,
			Error:   "network_error",
			Message: i18n.T("error.auth.network_error"),
		}, nil
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		return &ValidationResponse{
			Valid:   true,
			Message: i18n.T("error.auth.api_key_valid"),
		}, nil
	case http.StatusUnauthorized:
		return &ValidationResponse{
			Valid:   false,
			Error:   "invalid_api_key",
			Message: i18n.T("error.auth.api_key_invalid"),
		}, nil
	case http.StatusForbidden:
		return &ValidationResponse{
			Valid:   false,
			Error:   "no_go_subscription",
			Message: i18n.T("error.auth.no_go_subscription"),
		}, nil
	case http.StatusTooManyRequests:
		return &ValidationResponse{
			Valid:   false,
			Error:   "rate_limited",
			Message: i18n.T("error.auth.rate_limited"),
		}, nil
	default:
		return &ValidationResponse{
			Valid:   false,
			Error:   "server_error",
			Message: fmt.Sprintf(i18n.T("error.auth.server_error"), resp.StatusCode),
		}, nil
	}
}
