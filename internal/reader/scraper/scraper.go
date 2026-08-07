// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package scraper // import "miniflux.app/v2/internal/reader/scraper"

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"miniflux.app/v2/internal/config"
	"miniflux.app/v2/internal/reader/cloudflare"
	"miniflux.app/v2/internal/reader/encoding"
	"miniflux.app/v2/internal/reader/fetcher"
	"miniflux.app/v2/internal/reader/readability"
	"miniflux.app/v2/internal/urllib"

	"github.com/PuerkitoBio/goquery"
)

// ScrapeWebsite downloads pageURL and extracts content using scraper rules or readability.
// When Cloudflare bypass is enabled for policy, clearance cookies are applied (and solved
// on challenge) with a sticky proxy for retries. cookie/userAgent are the feed-level
// defaults passed into Bypass.DoRequest for clearance merge.
func ScrapeWebsite(requestBuilder *fetcher.RequestBuilder, pageURL, rules string, policy cloudflare.Policy, cookie, userAgent string) (baseURL string, extractedContent string, err error) {
	proxyURL, proxyErr := requestBuilder.ResolveProxyURL()
	proxyKey := "direct"
	proxyForSolver := ""
	if proxyErr == nil && proxyURL != nil {
		proxyKey = proxyURL.Redacted()
		proxyForSolver = proxyURL.String()
		requestBuilder = requestBuilder.WithLockedProxyURL(proxyURL)
	}

	bypass := cloudflare.Default()

	var httpResp *http.Response
	var clientErr error
	if bypass.Enabled(policy) {
		httpResp, clientErr = bypass.DoRequest(
			context.Background(),
			pageURL,
			proxyKey,
			proxyForSolver,
			policy,
			cookie,
			userAgent,
			func(cookie, ua string) (*http.Response, error) {
				b := requestBuilder.Clone().
					WithCookie(cookie).
					WithUserAgent(ua, config.Opts.HTTPClientUserAgent())
				return b.ExecuteRequest(pageURL)
			},
		)
	} else {
		httpResp, clientErr = requestBuilder.ExecuteRequest(pageURL)
	}

	responseHandler := fetcher.NewResponseHandler(httpResp, clientErr)
	defer responseHandler.Close()

	if localizedError := responseHandler.LocalizedError(); localizedError != nil {
		slog.Warn("Unable to scrape website", slog.String("website_url", pageURL), slog.Any("error", localizedError.Error()))
		return "", "", localizedError.Error()
	}

	if !isAllowedContentType(responseHandler.ContentType()) {
		return "", "", fmt.Errorf("scraper: this resource is not a HTML document (%s)", responseHandler.ContentType())
	}

	// The entry URL could redirect somewhere else.
	sameSite := urllib.Domain(pageURL) == urllib.Domain(responseHandler.EffectiveURL())
	pageURL = responseHandler.EffectiveURL()

	if rules == "" {
		rules = getPredefinedScraperRules(pageURL)
	}

	htmlDocumentReader, err := encoding.NewCharsetReader(
		responseHandler.Body(config.Opts.HTTPClientMaxBodySize()),
		responseHandler.ContentType(),
	)

	if err != nil {
		return "", "", fmt.Errorf("scraper: unable to read HTML document with charset reader: %v", err)
	}

	if sameSite && rules != "" {
		slog.Debug("Extracting content with custom rules",
			"url", pageURL,
			"rules", rules,
		)
		baseURL, extractedContent, err = findContentUsingCustomRules(htmlDocumentReader, rules)
	} else {
		slog.Debug("Extracting content with readability",
			"url", pageURL,
		)
		baseURL, extractedContent, err = readability.ExtractContent(htmlDocumentReader)
	}

	// The error returned by the extraction step used to be discarded, which
	// turned a failed extraction into a silent "no content" result: callers
	// kept the content provided by the feed without any way to know why.
	if err != nil {
		return "", "", fmt.Errorf("scraper: unable to extract content from %s: %w", pageURL, err)
	}

	if baseURL == "" {
		baseURL = pageURL
	} else {
		slog.Debug("Using base URL from HTML document", "base_url", baseURL)
	}

	if extractedContent == "" {
		slog.Debug("The scraper did not extract any content",
			"url", pageURL,
			"rules", rules,
		)
	}

	return baseURL, extractedContent, nil
}

func findContentUsingCustomRules(page io.Reader, rules string) (baseURL string, extractedContent string, err error) {
	document, err := goquery.NewDocumentFromReader(page)
	if err != nil {
		return "", "", err
	}

	if hrefValue, exists := document.FindMatcher(goquery.Single("head base")).Attr("href"); exists {
		hrefValue = strings.TrimSpace(hrefValue)
		if urllib.IsAbsoluteURL(hrefValue) {
			baseURL = hrefValue
		}
	}

	var buf strings.Builder
	document.Find(rules).Each(func(i int, s *goquery.Selection) {
		if content, err := goquery.OuterHtml(s); err == nil {
			buf.WriteString(content)
		}
	})

	return baseURL, buf.String(), nil
}

func getPredefinedScraperRules(websiteURL string) string {
	urlDomain := urllib.DomainWithoutWWW(websiteURL)

	if rules, ok := predefinedRules[urlDomain]; ok {
		return rules
	}
	return ""
}

func isAllowedContentType(contentType string) bool {
	contentType = strings.ToLower(contentType)
	return strings.HasPrefix(contentType, "text/html") ||
		strings.HasPrefix(contentType, "application/xhtml+xml")
}
