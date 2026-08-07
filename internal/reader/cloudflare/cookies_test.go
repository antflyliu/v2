// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package cloudflare_test

import (
	"reflect"
	"testing"

	"miniflux.app/v2/internal/reader/cloudflare"
)

func TestParseCookieHeader(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []cloudflare.Cookie
	}{
		{
			name: "empty",
			in:   "",
			want: nil,
		},
		{
			name: "single",
			in:   "cf_clearance=abc",
			want: []cloudflare.Cookie{{Name: "cf_clearance", Value: "abc"}},
		},
		{
			name: "multiple with spaces",
			in:   "a=1; b=2; c=three",
			want: []cloudflare.Cookie{
				{Name: "a", Value: "1"},
				{Name: "b", Value: "2"},
				{Name: "c", Value: "three"},
			},
		},
		{
			name: "value with equals",
			in:   "token=a=b=c",
			want: []cloudflare.Cookie{{Name: "token", Value: "a=b=c"}},
		},
		{
			name: "skips empty segments",
			in:   "a=1;; ; b=2",
			want: []cloudflare.Cookie{
				{Name: "a", Value: "1"},
				{Name: "b", Value: "2"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := cloudflare.ParseCookieHeader(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ParseCookieHeader(%q)=%v want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestFormatCookieHeader(t *testing.T) {
	tests := []struct {
		name    string
		cookies []cloudflare.Cookie
		want    string
	}{
		{
			name:    "empty",
			cookies: nil,
			want:    "",
		},
		{
			name:    "single",
			cookies: []cloudflare.Cookie{{Name: "cf_clearance", Value: "abc"}},
			want:    "cf_clearance=abc",
		},
		{
			name: "multiple",
			cookies: []cloudflare.Cookie{
				{Name: "a", Value: "1"},
				{Name: "b", Value: "2"},
			},
			want: "a=1; b=2",
		},
		{
			name: "skips empty names",
			cookies: []cloudflare.Cookie{
				{Name: "", Value: "x"},
				{Name: "ok", Value: "1"},
			},
			want: "ok=1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := cloudflare.FormatCookieHeader(tc.cookies)
			if got != tc.want {
				t.Fatalf("FormatCookieHeader()=%q want %q", got, tc.want)
			}
		})
	}
}

func TestMergeCookies(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		solver   []cloudflare.Cookie
		want     string
	}{
		{
			name:     "empty both",
			existing: "",
			solver:   nil,
			want:     "",
		},
		{
			name:     "existing only",
			existing: "session=keep",
			solver:   nil,
			want:     "session=keep",
		},
		{
			name:     "solver only",
			existing: "",
			solver:   []cloudflare.Cookie{{Name: "cf_clearance", Value: "xyz"}},
			want:     "cf_clearance=xyz",
		},
		{
			name:     "merge append new",
			existing: "session=keep",
			solver:   []cloudflare.Cookie{{Name: "cf_clearance", Value: "xyz"}},
			want:     "session=keep; cf_clearance=xyz",
		},
		{
			name:     "solver wins on conflict",
			existing: "cf_clearance=old; session=keep",
			solver:   []cloudflare.Cookie{{Name: "cf_clearance", Value: "new"}},
			want:     "cf_clearance=new; session=keep",
		},
		{
			name:     "skips empty solver names",
			existing: "a=1",
			solver: []cloudflare.Cookie{
				{Name: "", Value: "ignored"},
				{Name: "b", Value: "2"},
			},
			want: "a=1; b=2",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := cloudflare.MergeCookies(tc.existing, tc.solver)
			if got != tc.want {
				t.Fatalf("MergeCookies()=%q want %q", got, tc.want)
			}
		})
	}
}

func TestCookiesFromAPI(t *testing.T) {
	tests := []struct {
		name  string
		items []map[string]any
		want  []cloudflare.Cookie
	}{
		{
			name:  "nil",
			items: nil,
			want:  nil,
		},
		{
			name: "valid",
			items: []map[string]any{
				{"name": "cf_clearance", "value": "abc"},
				{"name": "session", "value": "1"},
			},
			want: []cloudflare.Cookie{
				{Name: "cf_clearance", Value: "abc"},
				{Name: "session", Value: "1"},
			},
		},
		{
			name: "skips invalid",
			items: []map[string]any{
				{"name": "", "value": "x"},
				{"value": "no-name"},
				{"name": "ok", "value": "1"},
				{"name": 123, "value": "bad-type"},
			},
			want: []cloudflare.Cookie{{Name: "ok", Value: "1"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := cloudflare.CookiesFromAPI(tc.items)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("CookiesFromAPI()=%v want %v", got, tc.want)
			}
		})
	}
}
