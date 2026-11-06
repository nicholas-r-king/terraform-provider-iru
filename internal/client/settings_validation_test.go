// SPDX-License-Identifier: MPL-2.0
package client

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestExistingSettingsValidationDiagnosticsAreScopedAndRedacted(t *testing.T) {
	for _, tc := range []struct{ endpoint, field string }{
		{"/api/v1/devices/" + testID, "tags"},
		{adePath + "/devices/" + testID, "override_dep_profile_id"},
		{adePath + "/devices/" + testID, "tags"},
		{blueprintRoutingPath, "enrollment_code.code"},
		{"/api/v1/blueprints/" + testID + "/assign-library-item", "library_item_id"},
		{"/api/v1/blueprints/" + testID + "/remove-library-item", "library_item_id"},
	} {
		t.Run(tc.field+tc.endpoint, func(t *testing.T) {
			calls := 0
			c := testClient(t, func(*http.Request) (*http.Response, error) {
				calls++
				body, _ := json.Marshal(map[string]any{"errors": []any{
					map[string]any{"field": tc.field, "code": "invalid", "message": "synthetic-private-message", "value": testToken},
					map[string]any{"field": "synthetic-private-field", "code": "required"},
					map[string]any{"field": "script", "code": "invalid"},
				}})
				return response(422, string(body)), nil
			})
			method := http.MethodPatch
			if strings.HasSuffix(tc.endpoint, "/assign-library-item") || strings.HasSuffix(tc.endpoint, "/remove-library-item") {
				method = http.MethodPost
			}
			_, err := c.request(context.Background(), method, tc.endpoint, map[string]string{"synthetic": "private"}, 200)
			if err == nil || calls != 1 || !strings.Contains(err.Error(), tc.field+"=invalid") || strings.Contains(err.Error(), "synthetic-private") || strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), "script=invalid") {
				t.Fatal("documented endpoint validation did not retain only scoped constant metadata")
			}
		})
	}
	for _, endpoint := range []string{
		"/api/v1/devices/", "/api/v1/devices/invalid", "/api/v1/devices/" + testID + "/commands",
		adePath + "/devices", adePath + "/devices/" + testID + "/renew",
		"/api/v1/blueprints/" + testID + "/other", "/api/v1/blueprints/invalid/assign-library-item",
		blueprintRoutingPath + "rules", blueprintRoutingPath + "?private=query",
	} {
		if validationFields(endpoint) != nil {
			t.Fatal("lookalike or unsupported endpoint gained validation-body inspection")
		}
	}
}
