// Package client talks to OpenCode's HTTP API for usage and model queries.
package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"errors"
	"github.com/emmmdty/token-usage/internal/i18n"
	"github.com/emmmdty/token-usage/internal/models"
)

type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

func NewClient(apiKey, baseURL string) *Client {
	if baseURL == "" {
		baseURL = "https://opencode.ai/zen/go/v1"
	}

	return &Client{
		apiKey:  apiKey,
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *Client) doRequest(endpoint string) ([]byte, error) {
	maxRetries := 3
	var retryAfter time.Duration
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			// Honor Retry-After from the previous 429 response when present.
			if retryAfter > backoff {
				backoff = retryAfter
			}
			time.Sleep(backoff)
		}

		req, err := http.NewRequest("GET", c.baseURL+endpoint, nil)
		if err != nil {
			return nil, err
		}

		req.Header.Set("Authorization", "Bearer "+c.apiKey)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if attempt < maxRetries {
				continue
			}
			return nil, err
		}

		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return nil, err
		}

		if resp.StatusCode == http.StatusOK {
			return body, nil
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			if attempt < maxRetries {
				if resp.StatusCode == http.StatusTooManyRequests {
					if d, err := time.ParseDuration(resp.Header.Get("Retry-After") + "s"); err == nil && d > 0 && d <= 5*time.Minute {
						retryAfter = d
					}
				}
				continue
			}
		}

		return nil, fmt.Errorf("%s", i18n.T("client.opencode.api_error", resp.StatusCode))
	}

	return nil, errors.New(i18n.T("client.opencode.max_retries"))
}

func (c *Client) GetUsage() (*models.Usage, error) {
	body, err := c.doRequest("/usage")
	if err != nil {
		return nil, err
	}

	var response struct {
		Usage models.Usage `json:"usage"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}

	return &response.Usage, nil
}

func (c *Client) GetModels() ([]models.Model, error) {
	body, err := c.doRequest("/models")
	if err != nil {
		return nil, err
	}

	var response struct {
		Data []models.Model `json:"data"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}

	return response.Data, nil
}
