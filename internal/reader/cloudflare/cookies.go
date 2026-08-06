// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package cloudflare // import "miniflux.app/v2/internal/reader/cloudflare"

import (
	"fmt"
	"strings"
)

// Cookie is a single name/value pair from a Cookie request header.
type Cookie struct {
	Name  string
	Value string
}

// ParseCookieHeader splits a Cookie header into name/value pairs.
// Empty segments and cookies without a name are skipped.
func ParseCookieHeader(h string) []Cookie {
	if h == "" {
		return nil
	}

	parts := strings.Split(h, ";")
	cookies := make([]Cookie, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, value, ok := strings.Cut(part, "=")
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if !ok {
			value = ""
		} else {
			value = strings.TrimSpace(value)
		}
		cookies = append(cookies, Cookie{Name: name, Value: value})
	}
	return cookies
}

// FormatCookieHeader joins cookies into a Cookie header value.
func FormatCookieHeader(cookies []Cookie) string {
	if len(cookies) == 0 {
		return ""
	}

	parts := make([]string, 0, len(cookies))
	for _, c := range cookies {
		if c.Name == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%s", c.Name, c.Value))
	}
	return strings.Join(parts, "; ")
}

// MergeCookies merges existing feed cookies with solver cookies.
// When both sides define the same name, the solver value wins.
// Insertion order is preserved: existing names first, then new solver names.
func MergeCookies(existingHeader string, solver []Cookie) string {
	m := make(map[string]string)
	order := make([]string, 0)

	for _, c := range ParseCookieHeader(existingHeader) {
		if c.Name == "" {
			continue
		}
		if _, ok := m[c.Name]; !ok {
			order = append(order, c.Name)
		}
		m[c.Name] = c.Value
	}

	for _, c := range solver {
		if c.Name == "" {
			continue
		}
		if _, ok := m[c.Name]; !ok {
			order = append(order, c.Name)
		}
		m[c.Name] = c.Value
	}

	parts := make([]string, 0, len(order))
	for _, n := range order {
		parts = append(parts, fmt.Sprintf("%s=%s", n, m[n]))
	}
	return strings.Join(parts, "; ")
}

// CookiesFromAPI converts loosely-typed solver cookie objects into Cookie values.
// Each item may provide "name" and "value" keys; missing or empty names are skipped.
func CookiesFromAPI(items []map[string]any) []Cookie {
	if len(items) == 0 {
		return nil
	}

	cookies := make([]Cookie, 0, len(items))
	for _, item := range items {
		name, _ := item["name"].(string)
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		value, _ := item["value"].(string)
		cookies = append(cookies, Cookie{Name: name, Value: value})
	}
	return cookies
}
