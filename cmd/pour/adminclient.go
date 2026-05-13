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
	adminDocsURL    = "https://ny4rl4th0t3p.github.io/pour/getting-started/#admin-token"
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
			return nil, fmt.Errorf(
				"admin token not found\n\n"+
					"  For information on how to set it, check %s",
				adminDocsURL,
			)
		}
		token = strings.TrimSpace(string(data))
		if token == "" {
			return nil, fmt.Errorf(
				"admin token file is empty: %s\n\n"+
					"  For information on how to set it, check %s",
				admin.TokenFile, adminDocsURL,
			)
		}
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
		return fmt.Errorf("could not reach daemon at %s: %w", c.baseURL, err)
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
		return fmt.Errorf("could not reach daemon at %s: %w", c.baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}
	if dest != nil {
		return json.NewDecoder(resp.Body).Decode(dest)
	}
	return nil
}

func (c *adminClient) deleteJSON(path string) error {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodDelete, c.baseURL+path, http.NoBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach daemon at %s: %w", c.baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}
	return nil
}
