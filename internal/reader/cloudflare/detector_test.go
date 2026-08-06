// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package cloudflare_test

import (
	"net/http"
	"testing"

	"miniflux.app/v2/internal/reader/cloudflare"
)

func TestIsChallenge(t *testing.T) {
	tests := []struct {
		name   string
		status int
		header http.Header
		want   bool
	}{
		{
			name:   "hard CF challenge",
			status: 403,
			header: http.Header{
				"Cf-Mitigated": []string{"challenge"},
				"Content-Type": []string{"text/html; charset=utf-8"},
			},
			want: true,
		},
		{
			name:   "plain 403",
			status: 403,
			header: http.Header{"Content-Type": []string{"text/html"}},
			want:   false,
		},
		{
			name:   "200 html",
			status: 200,
			header: http.Header{
				"Cf-Mitigated": []string{"challenge"},
				"Content-Type": []string{"text/html"},
			},
			want: false,
		},
		{
			name:   "403 challenge non-html",
			status: 403,
			header: http.Header{
				"Cf-Mitigated": []string{"challenge"},
				"Content-Type": []string{"application/json"},
			},
			want: false,
		},
		{
			name:   "403 challenge case-insensitive header",
			status: 403,
			header: http.Header{
				"Cf-Mitigated": []string{"Challenge"},
				"Content-Type": []string{"TEXT/HTML; charset=UTF-8"},
			},
			want: true,
		},
		{
			name:   "nil response",
			status: 0,
			header: nil,
			want:   false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var resp *http.Response
			if tc.name != "nil response" {
				resp = &http.Response{StatusCode: tc.status, Header: tc.header}
			}
			if got := cloudflare.IsChallenge(resp); got != tc.want {
				t.Fatalf("IsChallenge()=%v want %v", got, tc.want)
			}
		})
	}
}
