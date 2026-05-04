package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/ny4rl4th0t3p/pour/internal/admin"
)

const (
	adminURLEnv     = "POUR_ADMIN_URL"
	adminURLDefault = "http://localhost:8080"
)

type adminClient struct {
	baseURL string
	token   string
	http    *http.Client
}

func newAdminClient() (*adminClient, error) {
	baseURL := os.Getenv(adminURLEnv)
	if baseURL == "" {
		baseURL = adminURLDefault
	}
	baseURL = strings.TrimRight(baseURL, "/")

	token := os.Getenv(admin.TokenEnvVar)
	if token == "" {
		data, err := os.ReadFile(admin.TokenFile)
		if err != nil {
			return nil, fmt.Errorf("admin token not found: set %s or ensure %s exists",
				admin.TokenEnvVar, admin.TokenFile)
		}
		token = strings.TrimSpace(string(data))
	}

	return &adminClient{
		baseURL: baseURL,
		token:   token,
		http:    &http.Client{},
	}, nil
}

func (c *adminClient) getJSON(path string, dest any) error {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, c.baseURL+path, http.NoBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(dest)
}

func (c *adminClient) postJSON(path string, body, dest any) error {
	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, c.baseURL+path, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}
	if dest != nil {
		return json.NewDecoder(resp.Body).Decode(dest)
	}
	return nil
}
