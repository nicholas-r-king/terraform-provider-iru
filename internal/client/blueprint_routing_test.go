// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestBlueprintRoutingReadAndPatchContract(t *testing.T) {
	methods := []string{}
	c := testClient(t, func(req *http.Request) (*http.Response, error) {
		methods = append(methods, req.Method)
		if req.URL.Path != blueprintRoutingPath {
			t.Fatal("wrong routing endpoint")
		}
		if req.Method == "PATCH" {
			var body BlueprintRouting
			if json.NewDecoder(req.Body).Decode(&body) != nil || body.EnrollmentCode.Code == nil || *body.EnrollmentCode.Code != "000123" || body.EnrollmentCode.Active == nil || *body.EnrollmentCode.Active {
				t.Fatal("routing PATCH did not preserve code or explicit false")
			}
		}
		return response(200, `{"enrollment_code":{"code":"000123","is_active":false}}`), nil
	})
	value, err := c.GetBlueprintRouting(context.Background())
	if err != nil || *value.EnrollmentCode.Active {
		t.Fatal("read failed")
	}
	if _, err := c.UpdateBlueprintRouting(context.Background(), "000123", false); err != nil {
		t.Fatal(err)
	}
	if strings.Join(methods, ",") != "GET,PATCH" {
		t.Fatal("routing invented a create/delete endpoint")
	}
	if strings.Contains(fmt.Sprintf("%v %#v", value, value), "000123") {
		t.Fatal("routing code leaked in formatting")
	}
}
func TestBlueprintRoutingMalformedResponsesAndWriteFailures(t *testing.T) {
	for _, body := range []string{`{}`, `{"enrollment_code":null}`, `{"enrollment_code":{"code":null,"is_active":false}}`, `{"enrollment_code":{"code":"private-code"}}`, `{"enrollment_code":{"code":"","is_active":false}}`} {
		if _, err := decodeBlueprintRouting([]byte(body)); err == nil || strings.Contains(err.Error(), "private-code") {
			t.Fatal("invalid routing fields accepted or exposed")
		}
	}
	for _, status := range []int{400, 403, 429, 503} {
		calls := 0
		c := testClient(t, func(*http.Request) (*http.Response, error) {
			calls++
			return response(status, `{"enrollment_code":["private-code"]}`), nil
		})
		if _, err := c.UpdateBlueprintRouting(context.Background(), "000123", false); err == nil || calls != 1 || strings.Contains(err.Error(), "private-code") {
			t.Fatal("routing write replayed or exposed response")
		}
	}
}
