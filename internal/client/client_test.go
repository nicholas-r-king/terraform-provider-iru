// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const testID = "11111111-2222-4333-8444-555555555555"
const testCategory = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
const testToken = "synthetic-token-never-a-real-credential"

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func testClient(t *testing.T, transport roundTripFunc) *Client {
	t.Helper()
	c, err := New("https://synthetic.api.iru.com", testToken, &http.Client{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	c.sleep = func(context.Context, time.Duration) error { return nil }
	return c
}

func fixtureInput() ScriptInput {
	return ScriptInput{Name: "Synthetic audit", Script: "#!/bin/sh\nexit 0\n", ExecutionFrequency: "once"}
}

func fixtureJSON(t *testing.T, input ScriptInput) string {
	t.Helper()
	data, err := json.Marshal(Script{ID: testID, ScriptInput: input})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestValidateConfig(t *testing.T) {
	for _, host := range []string{"synthetic.api.kandji.io", "synthetic.api.eu.kandji.io", "synthetic.api.iru.com", "synthetic.api.eu.iru.com"} {
		if err := ValidateConfig("https://"+host+"/", testToken); err != nil {
			t.Errorf("valid host: %v", err)
		}
	}
	for _, base := range []string{"", "http://synthetic.api.iru.com", "https://example.com", "https://synthetic.api.iru.com.evil.example", "https://synthetic.api.iru.com:443", "https://user:password@synthetic.api.iru.com", "https://synthetic.api.iru.com/api/v1", "https://synthetic.api.iru.com?token=secret", "https://synthetic.api.iru.com#secret", "https://synthetic.api.iru.com?", "https://synthetic.api.iru.com#", "https://synthetic.api.iru.com/#"} {
		if err := ValidateConfig(base, testToken); err == nil {
			t.Errorf("accepted unsafe origin")
		}
	}
	for _, token := range []string{"", "token with spaces", "token\r\nInjected: header", "token\x00"} {
		if err := ValidateConfig("https://synthetic.api.iru.com", token); err == nil {
			t.Error("accepted invalid token")
		}
	}
}

func TestCanonicalOriginUsesDocumentedPath(t *testing.T) {
	c, err := New("https://synthetic.api.iru.com/", testToken, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != scriptsPath+"/"+testID || req.URL.Fragment != "" || req.URL.RawQuery != "" {
			t.Error("API path was not constructed from the validated origin")
		}
		return response(200, fixtureJSON(t, fixtureInput())), nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetScript(context.Background(), testID); err != nil {
		t.Fatal(err)
	}
}

func TestVersionedUserAgent(t *testing.T) {
	for _, tc := range []struct {
		name, version, want string
		invalid             bool
	}{
		{"release", "0.2.0", "terraform-provider-iru/0.2.0", false},
		{"prerelease", "v0.2.0-rc.1+build", "terraform-provider-iru/v0.2.0-rc.1+build", false},
		{"development fallback", "", "terraform-provider-iru/dev", false},
		{"header injection", "secret\r\nInjected: value", "", true},
		{"oversized", strings.Repeat("a", 65), "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c, err := NewWithVersion("https://synthetic.api.iru.com", testToken, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Header.Get("User-Agent") != tc.want {
					t.Error("provider build version did not reach User-Agent")
				}
				return response(200, fixtureJSON(t, fixtureInput())), nil
			})}, tc.version)
			if tc.invalid {
				if err == nil || calls != 0 {
					t.Fatal("unsafe version accepted")
				}
				if strings.Contains(err.Error(), "secret") {
					t.Fatal("unsafe version echoed in error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := c.GetScript(context.Background(), testID); err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatal("unexpected request count")
			}
		})
	}
}

func TestCRUDWireContract(t *testing.T) {
	ctx := context.Background()
	input := fixtureInput()
	wantMethods := []string{"POST", "GET", "PATCH", "DELETE"}
	index := 0
	c := testClient(t, func(r *http.Request) (*http.Response, error) {
		if index >= len(wantMethods) {
			t.Fatal("unexpected extra request")
		}
		wantMethod := wantMethods[index]
		index++
		if r.Method != wantMethod {
			t.Errorf("method = %s, want %s", r.Method, wantMethod)
		}
		wantPath := scriptsPath
		if r.Method != "POST" {
			wantPath += "/" + testID
		}
		if r.URL.Path != wantPath || r.URL.RawQuery != "" || r.URL.Host != "synthetic.api.iru.com" {
			t.Errorf("unexpected request target")
		}
		if r.Header.Get("Authorization") != "Bearer "+testToken || r.Header.Get("Accept") != "application/json" {
			t.Error("missing documented authentication/accept headers")
		}
		if r.Method == "POST" || r.Method == "PATCH" {
			if r.Header.Get("Content-Type") != "application/json" {
				t.Error("wrong content type")
			}
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			wantFields := 7
			if r.Method == "POST" {
				wantFields = 6
				if _, exists := payload["remediation_script"]; exists {
					t.Error("create must omit absent remediation content")
				}
			} else if payload["remediation_script"] != "" {
				t.Error("update must explicitly request remediation removal")
			}
			if len(payload) != wantFields || payload["name"] != input.Name || payload["script"] != input.Script || payload["execution_frequency"] != "once" || payload["active"] != false || payload["restart"] != false || payload["show_in_self_service"] != false {
				t.Error("request does not match documented script fields")
			}
		}
		switch r.Method {
		case "POST":
			return response(201, fixtureJSON(t, input)), nil
		case "DELETE":
			return response(204, ""), nil
		default:
			return response(200, fixtureJSON(t, input)), nil
		}
	})
	created, err := c.CreateScript(ctx, input)
	if err != nil || created.ID != testID {
		t.Fatalf("create failed: %v", err)
	}
	read, err := c.GetScript(ctx, created.ID)
	if err != nil || read.Script != input.Script {
		t.Fatalf("read failed: %v", err)
	}
	if _, err := c.UpdateScript(ctx, read.ID, input); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteScript(ctx, read.ID); err != nil {
		t.Fatal(err)
	}
	if index != len(wantMethods) {
		t.Error("missing lifecycle request")
	}
}

func TestScriptConstraints(t *testing.T) {
	category := testCategory
	for _, frequency := range []string{"once", "every_15_min", "every_day", "no_enforcement"} {
		input := fixtureInput()
		input.ExecutionFrequency = frequency
		if frequency == "no_enforcement" {
			input.ShowInSelfService = true
			input.SelfServiceCategoryID = &category
		}
		if err := input.Validate(); err != nil {
			t.Errorf("valid frequency rejected: %v", err)
		}
	}
	cases := []struct {
		name   string
		mutate func(*ScriptInput)
	}{
		{"blank name", func(v *ScriptInput) { v.Name = " " }},
		{"blank script", func(v *ScriptInput) { v.Script = "\n" }},
		{"frequency", func(v *ScriptInput) { v.ExecutionFrequency = "hourly" }},
		{"no enforcement without self service", func(v *ScriptInput) { v.ExecutionFrequency = "no_enforcement" }},
		{"self service without category", func(v *ScriptInput) { v.ShowInSelfService = true }},
		{"invalid category", func(v *ScriptInput) { v.ShowInSelfService = true; x := "invalid"; v.SelfServiceCategoryID = &x }},
		{"hidden category", func(v *ScriptInput) { v.SelfServiceCategoryID = &category }},
		{"hidden recommendation", func(v *ScriptInput) { x := true; v.SelfServiceRecommended = &x }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := fixtureInput()
			tc.mutate(&input)
			c := testClient(t, func(*http.Request) (*http.Response, error) {
				t.Fatal("invalid configuration reached transport")
				return nil, nil
			})
			if _, err := c.CreateScript(context.Background(), input); err == nil {
				t.Fatal("invalid input accepted")
			}
		})
	}
}

func TestSelfServicePayload(t *testing.T) {
	input := fixtureInput()
	input.ShowInSelfService, input.ExecutionFrequency = true, "no_enforcement"
	category, recommended := testCategory, false
	input.SelfServiceCategoryID, input.SelfServiceRecommended = &category, &recommended
	c := testClient(t, func(r *http.Request) (*http.Response, error) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["self_service_category_id"] != testCategory || payload["self_service_recommended"] != false {
			t.Error("Self Service fields lost")
		}
		return response(201, fixtureJSON(t, input)), nil
	})
	if _, err := c.CreateScript(context.Background(), input); err != nil {
		t.Fatal(err)
	}
}

func TestAuthAndBodyAreNeverInErrorsOrClientFormatting(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 422, 429, 500, 503} {
		c := testClient(t, func(*http.Request) (*http.Response, error) {
			return response(status, testToken+" confidential script body"), nil
		})
		_, err := c.GetScript(context.Background(), testID)
		if err == nil {
			t.Fatal("expected status error")
		}
		for _, printed := range []string{err.Error(), fmt.Sprintf("%v %+v %#v", c, c, c), fmt.Sprintf("%v %+v %#v", *c, *c, *c)} {
			if strings.Contains(printed, testToken) || strings.Contains(printed, "confidential script") || strings.Contains(printed, "Authorization") {
				t.Fatal("sensitive authentication/body content leaked")
			}
		}
		if IsNotFound(err) != (status == 404) {
			t.Error("incorrect 404 mapping")
		}
	}
	c := testClient(t, func(*http.Request) (*http.Response, error) {
		return nil, errors.New("Authorization: Bearer " + testToken)
	})
	_, err := c.CreateScript(context.Background(), fixtureInput())
	if err == nil || strings.Contains(err.Error(), testToken) {
		t.Fatal("transport error leaked a credential")
	}
}

func TestBoundedReadRetriesAndNoWriteReplay(t *testing.T) {
	for _, method := range []string{"GET", "POST", "PATCH", "DELETE"} {
		for _, status := range []int{429, 502, 503, 504, 401, 403} {
			count := 0
			c := testClient(t, func(*http.Request) (*http.Response, error) { count++; return response(status, "withheld"), nil })
			_, err := c.request(context.Background(), method, scriptsPath, nil, 200)
			if err == nil {
				t.Fatal("expected error")
			}
			want := 1
			if method == "GET" && (status == 429 || status == 502 || status == 503 || status == 504) {
				want = 3
			}
			if count != want {
				t.Errorf("%s/%d attempts=%d want=%d", method, status, count, want)
			}
		}
	}
	count := 0
	c := testClient(t, func(*http.Request) (*http.Response, error) {
		count++
		result := response(429, "withheld")
		result.Header.Set("Retry-After", "60")
		return result, nil
	})
	_, _ = c.GetScript(context.Background(), testID)
	if count != 1 {
		t.Error("retried before long Retry-After elapsed")
	}
	count = 0
	c = testClient(t, func(*http.Request) (*http.Response, error) {
		count++
		if count == 1 {
			return response(503, "withheld"), nil
		}
		return response(200, fixtureJSON(t, fixtureInput())), nil
	})
	if _, err := c.GetScript(context.Background(), testID); err != nil || count != 2 {
		t.Error("read did not recover within bound")
	}
}

func TestRetryDelayAndCancellation(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for _, tc := range []struct {
		header  string
		allowed bool
	}{{"0", true}, {"2", true}, {"5", true}, {"6", false}, {"-1", false}, {"garbage", false}, {now.Add(3 * time.Second).Format(http.TimeFormat), true}, {now.Add(60 * time.Second).Format(http.TimeFormat), false}} {
		delay, ok := retryDelay(tc.header, 0, now)
		if ok != tc.allowed || (ok && (delay < 0 || delay > 5*time.Second)) {
			t.Error("bad Retry-After bound")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Error("wait ignored cancellation")
	}
	c := testClient(t, func(*http.Request) (*http.Response, error) { return nil, context.Canceled })
	if _, err := c.GetScript(ctx, testID); !errors.Is(err, context.Canceled) {
		t.Error("request lost cancellation")
	}
}

func TestRedirectIsRefused(t *testing.T) {
	count := 0
	c := testClient(t, func(*http.Request) (*http.Response, error) {
		count++
		result := response(302, "")
		result.Header.Set("Location", "https://other.api.iru.com/stolen")
		return result, nil
	})
	_, err := c.GetScript(context.Background(), testID)
	if err == nil || count != 1 {
		t.Fatal("redirect followed")
	}
	if c.http.Timeout != 30*time.Second {
		t.Error("missing finite timeout")
	}
}

func TestResponseValidationAndSizeLimit(t *testing.T) {
	valid := fixtureJSON(t, fixtureInput())
	cases := []string{"null", "[]", "{", "{}", strings.Replace(valid, `"active":false,`, "", 1), strings.Replace(valid, `"script":"#!/bin/sh\nexit 0\n"`, `"script":null`, 1), strings.Replace(valid, testID, "not-an-id", 1), strings.Replace(valid, `"once"`, `"hourly"`, 1)}
	for _, body := range cases {
		if _, err := decodeScript([]byte(body)); err == nil {
			t.Error("malformed response accepted")
		}
	}
	legacy := strings.Replace(valid, `"once"`, `"no_enforcement"`, 1)
	if _, err := decodeScript([]byte(legacy)); err == nil {
		t.Error("legacy missing Self Service metadata accepted")
	}
	c := testClient(t, func(*http.Request) (*http.Response, error) {
		return response(200, strings.Repeat("x", responseLimit+1)), nil
	})
	if _, err := c.GetScript(context.Background(), testID); err == nil {
		t.Error("oversized response accepted")
	}
	c = testClient(t, func(*http.Request) (*http.Response, error) {
		return response(200, strings.Replace(valid, testID, testCategory, 1)), nil
	})
	if _, err := c.GetScript(context.Background(), testID); err == nil {
		t.Error("mismatched response ID accepted")
	}
}

func TestInvalidIDsDoNotReachTransportAndDelete404Succeeds(t *testing.T) {
	c := testClient(t, func(*http.Request) (*http.Response, error) { t.Fatal("invalid ID reached transport"); return nil, nil })
	if _, err := c.GetScript(context.Background(), "../bad"); err == nil {
		t.Error("bad read ID")
	}
	if _, err := c.UpdateScript(context.Background(), "../bad", fixtureInput()); err == nil {
		t.Error("bad update ID")
	}
	if err := c.DeleteScript(context.Background(), "../bad"); err == nil {
		t.Error("bad delete ID")
	}
	c = testClient(t, func(*http.Request) (*http.Response, error) { return response(404, ""), nil })
	if err := c.DeleteScript(context.Background(), testID); err != nil {
		t.Error("already-absent deletion must succeed")
	}
}
