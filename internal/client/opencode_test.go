package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGetUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		response := map[string]interface{}{
			"usage": map[string]interface{}{
				"rolling": map[string]interface{}{
					"status":   "ok",
					"percent":  35,
					"resetsAt": time.Now().Add(8 * time.Hour).Format(time.RFC3339),
				},
				"weekly": map[string]interface{}{
					"status":   "ok",
					"percent":  12,
					"resetsAt": time.Now().Add(5 * 24 * time.Hour).Format(time.RFC3339),
				},
				"monthly": map[string]interface{}{
					"status":   "ok",
					"percent":  8,
					"resetsAt": time.Now().Add(23 * 24 * time.Hour).Format(time.RFC3339),
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL)
	usage, err := client.GetUsage()
	if err != nil {
		t.Fatalf("failed to get usage: %v", err)
	}

	if usage.Rolling.Percent != 35 {
		t.Errorf("expected rolling percent 35, got %d", usage.Rolling.Percent)
	}
}

func TestGetModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"data": []map[string]interface{}{
				{
					"id":   "mimo-v2.5",
					"name": "MiMo-V2.5",
					"pricing": map[string]interface{}{
						"input":  0.14,
						"output": 0.28,
					},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL)
	models, err := client.GetModels()
	if err != nil {
		t.Fatalf("failed to get models: %v", err)
	}

	if len(models) != 1 {
		t.Errorf("expected 1 model, got %d", len(models))
	}
}
