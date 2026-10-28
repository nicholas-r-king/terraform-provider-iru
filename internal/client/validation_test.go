// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestValidationIssueShapesAndRedaction(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       []validationIssue
	}{
		{"field messages", `{"remediation_script":["synthetic-confidential-value"],"name":["synthetic-confidential-value"]}`, []validationIssue{{"name", "rejected"}, {"remediation_script", "rejected"}}},
		{"explicit codes", `{"name":[{"code":"required","message":"synthetic-confidential-value"}],"script":{"code":"blank","value":"synthetic-confidential-value"}}`, []validationIssue{{"name", "required"}, {"script", "blank"}}},
		{"errors field map", `{"errors":{"execution_frequency":[{"code":"invalid_choice","message":"synthetic-confidential-value"}]}}`, []validationIssue{{"execution_frequency", "invalid_choice"}}},
		{"errors list", `{"errors":[{"field":"active","code":"invalid_type","message":"synthetic-confidential-value"}]}`, []validationIssue{{"active", "invalid_type"}}},
		{"top level list", `[{"field":"script","code":"min_length"}]`, []validationIssue{{"script", "min_length"}}},
		{"single error", `{"field":"name","code":"max_length"}`, []validationIssue{{"name", "max_length"}}},
		{"message only error", `{"field":"script","message":"synthetic-confidential-value"}`, []validationIssue{{"script", "rejected"}}},
		{"unrecognized code", `{"name":{"code":"synthetic-confidential-value"}}`, []validationIssue{{"name", "rejected"}}},
		{"request validation", `{"non_field_errors":[{"code":"invalid","message":"synthetic-confidential-value"}]}`, []validationIssue{{"request", "invalid"}}},
		{"duplicates sorted", `{"errors":[{"field":"script","code":"blank"},{"field":"name","code":"required"},{"field":"script","code":"blank"}]}`, []validationIssue{{"name", "required"}, {"script", "blank"}}},
		{"unknown key", `{"synthetic-confidential-value":["required"],"api_token":["synthetic-confidential-value"]}`, nil},
		{"unknown field", `{"errors":[{"field":"synthetic-confidential-value","code":"required"}]}`, nil},
		{"detail ignored", `{"detail":"synthetic-confidential-value"}`, nil},
		{"message subtree ignored", `{"message":{"script":[{"code":"required"}]}}`, nil},
		{"value subtree ignored", `{"value":{"script":[{"code":"required"}]}}`, nil},
		{"no messages", `{"name":[],"script":null,"restart":false}`, nil},
		{"malformed", `{"script":[`, nil},
		{"trailing input", `{"script":["secret"]}unexpected`, nil},
		{"HTML", `<html>synthetic-confidential-value</html>`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issues := readValidationIssues(strings.NewReader(tc.body), http.MethodPost, scriptsPath, 400)
			if len(issues) != len(tc.want) || (len(issues) > 0 && !reflect.DeepEqual(issues, tc.want)) {
				t.Fatalf("unexpected safe classification: %#v", issues)
			}
			err := &APIError{Method: http.MethodPost, StatusCode: 400, validation: issues}
			for _, rendered := range []string{err.Error(), fmt.Sprintf("%v %+v %#v", err, err, err)} {
				if strings.Contains(rendered, "synthetic-confidential-value") {
					t.Fatal("server content reached the error")
				}
			}
		})
	}
}

func TestValidationClassificationsAreFinite(t *testing.T) {
	for _, code := range []string{"required", "blank", "null", "invalid", "invalid_choice", "invalid_type", "max_length", "min_length", "max_value", "min_value", "unique"} {
		if validationCode(code) != code {
			t.Error("missing explicit safe classification")
		}
	}
	for _, code := range []any{nil, true, 1, []any{"required"}, map[string]any{"code": "required"}, "required\nsecret", "Required", "synthetic-secret"} {
		if validationCode(code) != "rejected" {
			t.Error("accepted an unrecognized classification")
		}
	}
}

type countingValidationBody struct {
	io.Reader
	read, closed int
}

func (b *countingValidationBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	b.read += n
	return n, err
}

func (b *countingValidationBody) Close() error { b.closed++; return nil }

func TestNonValidationBodiesAreNotRead(t *testing.T) {
	for _, tc := range []struct {
		method, endpoint string
		status           int
	}{
		{http.MethodPost, scriptsPath, 401}, {http.MethodPost, scriptsPath, 403},
		{http.MethodPost, scriptsPath, 429}, {http.MethodPost, scriptsPath, 503},
		{http.MethodPost, scriptsPath, 307}, {http.MethodGet, scriptsPath, 400},
		{http.MethodDelete, scriptsPath, 400}, {http.MethodPost, "/api/v1/unmanaged", 400},
		{http.MethodPost, scriptsPath + "/upload", 400}, {http.MethodPost, scriptsPath + "?name=secret", 400},
	} {
		body := &countingValidationBody{Reader: strings.NewReader(`{"name":["synthetic-secret"]}`)}
		if len(readValidationIssues(body, tc.method, tc.endpoint, tc.status)) != 0 || body.read != 0 {
			t.Fatal("non-validation error body was read")
		}
	}
}

func TestValidationParsingLimits(t *testing.T) {
	oversized := `{"name":["` + strings.Repeat("x", validationResponseLimit) + `"]}`
	body := &countingValidationBody{Reader: strings.NewReader(oversized)}
	if len(readValidationIssues(body, http.MethodPost, scriptsPath, 400)) != 0 || body.read != validationResponseLimit+1 {
		t.Fatal("oversized response was not bounded and discarded")
	}
	deep := `{"errors":` + strings.Repeat(`[`, 8) + `{"field":"name","code":"required"}` + strings.Repeat(`]`, 8) + `}`
	if len(readValidationIssues(strings.NewReader(deep), http.MethodPost, scriptsPath, 400)) != 0 {
		t.Fatal("excessive nesting was traversed")
	}
	wide := `{"name":[` + strings.Repeat(`"synthetic-secret",`, 300) + `"synthetic-secret"]}`
	if len(readValidationIssues(strings.NewReader(wide), http.MethodPost, scriptsPath, 400)) != 0 {
		t.Fatal("node budget did not discard partial results")
	}
	var entries []any
	for _, code := range []string{"required", "blank", "invalid", "null", "unique", "max_length", "min_length", "invalid_type", "invalid_choice"} {
		entries = append(entries, map[string]any{"field": "name", "code": code}, map[string]any{"field": "script", "code": code})
	}
	encoded, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	issues := readValidationIssues(strings.NewReader(string(encoded)), http.MethodPatch, scriptsPath+"/"+testID, 422)
	if len(issues) != validationIssueLimit {
		t.Fatal("reported issues are not capped")
	}
}

func TestValidationFieldsAreEndpointSpecific(t *testing.T) {
	for _, tc := range []struct{ endpoint, body, field string }{
		{appsPath, `{"file_key":[{"code":"required"}],"script":[{"code":"required"}]}`, "file_key"},
		{profilesPath, `{"file":[{"code":"required"}],"file_key":[{"code":"required"}]}`, "file"},
		{scriptsPath, `{"script":[{"code":"required"}],"file_key":[{"code":"required"}]}`, "script"},
	} {
		issues := readValidationIssues(strings.NewReader(tc.body), http.MethodPost, tc.endpoint, 400)
		if !reflect.DeepEqual(issues, []validationIssue{{tc.field, "required"}}) {
			t.Fatal("field from a different endpoint was reported")
		}
	}
}

type failingValidationReader struct{}

func (failingValidationReader) Read([]byte) (int, error) { return 0, errors.New("synthetic-secret") }

func TestValidationReadFailureRetainsOnlyStatus(t *testing.T) {
	if issues := readValidationIssues(failingValidationReader{}, http.MethodPost, scriptsPath, 400); len(issues) != 0 {
		t.Fatal("read failure leaked into a validation diagnostic")
	}
}

func TestValidationAPIErrorPreservesStatusAndNeverReplays(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPatch} {
		for _, status := range []int{400, 422} {
			body := &countingValidationBody{Reader: strings.NewReader(`{"remediation_script":[{"code":"blank","message":"` + testToken + ` confidential script"}],"Authorization":"` + testToken + `"}`)}
			calls := 0
			c := testClient(t, func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: status, Body: body, Header: make(http.Header)}, nil
			})
			_, err := c.request(context.Background(), method, scriptsPath, fixtureInput(), 201)
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != status || apiErr.Method != method || calls != 1 || body.closed != 1 {
				t.Fatal("status, body lifecycle or no-replay behavior changed")
			}
			if !strings.Contains(err.Error(), "remediation_script=blank") || strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), "confidential script") {
				t.Fatal("safe classification missing or sensitive response included")
			}
		}
	}
}

func FuzzValidationIssuesNeverRetainResponseValues(f *testing.F) {
	for _, seed := range []string{`{"script":["secret"]}`, `{"errors":[{"field":"name","code":"required"}]}`, `{"message":{"name":"secret"}}`, `null`, `[]`, `<html>`, `{"name":"\u001b[31msecret"}`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		issues := readValidationIssues(strings.NewReader(input), http.MethodPost, scriptsPath, 400)
		if len(issues) > validationIssueLimit {
			t.Fatal("unbounded diagnostic")
		}
		allowed := validationFields(scriptsPath)
		for _, issue := range issues {
			known := issue.field == "request"
			for _, canonical := range allowed {
				known = known || canonical == issue.field
			}
			if !known || (issue.code != "rejected" && validationCode(issue.code) != issue.code) {
				t.Fatal("untrusted content retained")
			}
		}
	})
}
