// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package cloudflare // import "miniflux.app/v2/internal/reader/cloudflare"

import (
	"net/http"
	"strings"
)

// IsChallenge reports whether resp looks like a Cloudflare bot interstitial.
// Hard rule only (v1): 403 + cf-mitigated:challenge + text/html.
func IsChallenge(resp *http.Response) bool {
	if resp == nil {
		return false
	}
	if resp.StatusCode != http.StatusForbidden {
		return false
	}
	if !strings.EqualFold(resp.Header.Get("cf-mitigated"), "challenge") {
		return false
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	return strings.HasPrefix(ct, "text/html")
}
