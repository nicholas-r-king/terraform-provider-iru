// SPDX-License-Identifier: MPL-2.0

// Package client implements only the documented Iru Custom Scripts API.
// It deliberately does not log headers, URLs, request/response bodies, or tokens.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const scriptsPath = "/api/v1/library/custom-scripts"
const responseLimit = 2 * 1024 * 1024

var tenantHost = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.api\.(?:eu\.)?(?:kandji\.io|iru\.com)$`)
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}(?:-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}$`)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
	sleep   func(context.Context, time.Duration) error
}

// String and GoString prevent accidental formatting of the client's token.
func (Client) String() string   { return "Iru client (credentials redacted)" }
func (Client) GoString() string { return "Iru client (credentials redacted)" }

// ValidateConfig never returns the supplied configuration in an error.
func ValidateConfig(baseURL, token string) error {
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme != "https" || !tenantHost.MatchString(u.Host) || u.User != nil ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || strings.Contains(baseURL, "#") || u.ForceQuery {
		return errors.New("api_url must be an HTTPS tenant origin: <tenant>.api.kandji.io, <tenant>.api.eu.kandji.io, <tenant>.api.iru.com, or <tenant>.api.eu.iru.com; paths, ports, credentials, queries, and fragments are not accepted")
	}
	if token == "" || strings.IndexFunc(token, unicode.IsSpace) >= 0 || strings.IndexFunc(token, unicode.IsControl) >= 0 {
		return errors.New("api_token must be nonempty and contain no whitespace or control characters; load it with IRU_API_TOKEN")
	}
	return nil
}

// New accepts a custom HTTP transport for synthetic tests. The caller's client
// is copied; redirects are always refused to prevent forwarding credentials.
func New(baseURL, token string, httpClient *http.Client) (*Client, error) {
	if err := ValidateConfig(baseURL, token); err != nil {
		return nil, err
	}
	hc := http.Client{Timeout: 30 * time.Second}
	if httpClient != nil {
		hc = *httpClient
	}
	if hc.Timeout <= 0 || hc.Timeout > 30*time.Second {
		hc.Timeout = 30 * time.Second
	}
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	origin, _ := url.Parse(baseURL) // ValidateConfig already parsed and checked it.
	canonical := (&url.URL{Scheme: origin.Scheme, Host: origin.Host}).String()
	return &Client{baseURL: canonical, token: token, http: &hc, sleep: sleep}, nil
}

func sleep(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// APIError contains only the method and status, never raw response content.
type APIError struct {
	Method     string
	StatusCode int
}

func (e *APIError) Error() string {
	hint := "check endpoint permissions and API availability"
	switch e.StatusCode {
	case 400, 422:
		hint = "Iru rejected the Custom Script fields; check the documented constraints"
	case 401:
		hint = "check IRU_API_TOKEN"
	case 403:
		hint = "the token needs permission for this Custom Scripts operation"
	case 404:
		hint = "the Custom Script was not found"
	case 429:
		hint = "rate limit reached; retry later"
	case 301, 302, 303, 307, 308:
		hint = "redirect refused; verify the tenant API origin"
	}
	return fmt.Sprintf("Iru %s Custom Scripts returned HTTP %d: %s (response body withheld)", e.Method, e.StatusCode, hint)
}

func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

func ValidID(id string) bool { return uuidPattern.MatchString(id) }

func (c *Client) request(ctx context.Context, method, path string, payload any, success int) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	var body []byte
	if payload != nil {
		var err error
		body, err = json.Marshal(payload)
		if err != nil {
			return nil, errors.New("could not encode Custom Script request (content withheld)")
		}
		if len(body) > responseLimit {
			return nil, errors.New("Custom Script request exceeds the 2 MiB safety limit")
		}
	}
	// Only reads are replayed. A failed write can have reached Iru; automatically
	// replaying POST/PATCH/DELETE would risk duplicate or ambiguous mutations.
	attempts := 1
	if method == http.MethodGet {
		attempts = 3
	}
	for attempt := 0; attempt < attempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
		if err != nil {
			return nil, errors.New("could not construct Iru request")
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "terraform-provider-iru/dev")
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.http.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("Iru request interrupted: %w", ctx.Err())
			}
			return nil, errors.New("Iru transport failed or timed out (details withheld); a write may have succeeded; inspect/import the item before retrying creation")
		}
		if resp.StatusCode != success {
			// Do not read error bodies: they may echo tokens or script contents.
			resp.Body.Close()
			retryable := resp.StatusCode == 429 || resp.StatusCode == 502 || resp.StatusCode == 503 || resp.StatusCode == 504
			if retryable && attempt+1 < attempts {
				delay, allowed := retryDelay(resp.Header.Get("Retry-After"), attempt, time.Now())
				if allowed {
					if err := c.sleep(ctx, delay); err != nil {
						return nil, fmt.Errorf("Iru retry interrupted: %w", err)
					}
					continue
				}
			}
			return nil, &APIError{Method: method, StatusCode: resp.StatusCode}
		}
		if success == http.StatusNoContent {
			resp.Body.Close()
			return nil, nil
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, responseLimit+1))
		resp.Body.Close()
		if readErr != nil {
			return nil, errors.New("could not read Iru response (content withheld)")
		}
		if len(data) > responseLimit {
			return nil, errors.New("Iru response exceeds the 2 MiB safety limit")
		}
		return data, nil
	}
	return nil, errors.New("Iru retry limit reached")
}

// Retry-After beyond five seconds is surfaced to the user, rather than ignored
// or followed by an early retry. At most two waits are performed per GET.
func retryDelay(header string, attempt int, now time.Time) (time.Duration, bool) {
	if header == "" {
		return time.Duration(1<<attempt) * 250 * time.Millisecond, true
	}
	var delay time.Duration
	if seconds, err := strconv.ParseInt(header, 10, 32); err == nil {
		if seconds < 0 || seconds > 5 {
			return 0, false
		}
		delay = time.Duration(seconds) * time.Second
	} else if date, err := http.ParseTime(header); err == nil {
		delay = date.Sub(now)
	} else {
		return 0, false
	}
	if delay < 0 {
		delay = 0
	}
	return delay, delay <= 5*time.Second
}
