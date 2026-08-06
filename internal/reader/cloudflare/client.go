// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package cloudflare // import "miniflux.app/v2/internal/reader/cloudflare"

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client calls a trusted local clearance solver over HTTP.
//
// CLOUDFLARE_BYPASS_URL is expected to point at a local service (typically
// 127.0.0.1). This client uses a normal http.Client dial path and must not
// go through the fetcher private-network block that protects feed fetches.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

// SolveRequest is the input for POST /v1/clearance.
type SolveRequest struct {
	WebsiteURL string
	Proxy      string
	Timeout    time.Duration
	UserAgent  string
}

// SolveResponse is the parsed solver result.
// Business failures (ok:false) are returned with error == nil.
// Transport, HTTP status, and decode failures return a non-nil error.
type SolveResponse struct {
	OK        bool
	Cookies   []Cookie
	UserAgent string
	FinalURL  string
	ExpiresAt time.Time // zero if absent
	ErrorCode string
	ErrorDesc string
}

type solveRequestBody struct {
	WebsiteURL string  `json:"websiteURL"`
	Proxy      string  `json:"proxy,omitempty"`
	TimeoutSec float64 `json:"timeoutSec,omitempty"`
	UserAgent  string  `json:"userAgent,omitempty"`
}

type solveCookieBody struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type solveResponseBody struct {
	OK               bool              `json:"ok"`
	Cookies          []solveCookieBody `json:"cookies"`
	UserAgent        string            `json:"userAgent"`
	FinalURL         string            `json:"finalURL"`
	ExpiresAt        *int64            `json:"expiresAt"`
	ErrorCode        string            `json:"errorCode"`
	ErrorDescription string            `json:"errorDescription"`
}

// Solve posts to {BaseURL}/v1/clearance and returns the parsed response.
func (c *Client) Solve(ctx context.Context, req SolveRequest) (*SolveResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("cloudflare: client is nil")
	}
	base := strings.TrimSpace(c.BaseURL)
	if base == "" {
		return nil, fmt.Errorf("cloudflare: base URL is empty")
	}
	if strings.TrimSpace(req.WebsiteURL) == "" {
		return nil, fmt.Errorf("cloudflare: websiteURL is required")
	}

	endpoint := strings.TrimRight(base, "/") + "/v1/clearance"
	body := solveRequestBody{
		WebsiteURL: req.WebsiteURL,
		Proxy:      req.Proxy,
		UserAgent:  req.UserAgent,
	}
	if req.Timeout > 0 {
		body.TimeoutSec = req.Timeout.Seconds()
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("cloudflare: encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("cloudflare: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	httpClient := c.HTTPClient
	if httpClient == nil {
		// Normal dial: solver is a trusted local service (CLOUDFLARE_BYPASS_URL).
		httpClient = http.DefaultClient
	}

	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("cloudflare: solve request: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(httpResp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("cloudflare: read response: %w", err)
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		snippet := strings.TrimSpace(string(respBody))
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return nil, fmt.Errorf("cloudflare: solver status %d: %s", httpResp.StatusCode, snippet)
	}

	var parsed solveResponseBody
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("cloudflare: decode response: %w", err)
	}

	out := &SolveResponse{
		OK:        parsed.OK,
		UserAgent: parsed.UserAgent,
		FinalURL:  parsed.FinalURL,
		ErrorCode: parsed.ErrorCode,
		ErrorDesc: parsed.ErrorDescription,
	}
	if parsed.ExpiresAt != nil && *parsed.ExpiresAt > 0 {
		out.ExpiresAt = time.Unix(*parsed.ExpiresAt, 0)
	}
	if len(parsed.Cookies) > 0 {
		out.Cookies = make([]Cookie, 0, len(parsed.Cookies))
		for _, c := range parsed.Cookies {
			name := strings.TrimSpace(c.Name)
			if name == "" {
				continue
			}
			out.Cookies = append(out.Cookies, Cookie{Name: name, Value: c.Value})
		}
	}
	return out, nil
}
