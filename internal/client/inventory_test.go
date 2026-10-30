// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestInventoryProjectionAndEndpointAllowlist(t *testing.T) {
	for _, kind := range []string{"blueprints", "tags", "custom_scripts", "custom_apps", "custom_profiles", "in_house_apps", "ade_integrations", "self_service_categories"} {
		t.Run(kind, func(t *testing.T) {
			endpoint, nameField, array := inventoryEndpoint(kind)
			calls := 0
			c := testClient(t, func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Method != "GET" || req.URL.Path != endpoint {
					t.Fatal("inventory attempted a write or wrong endpoint")
				}
				item := map[string]any{"id": testID, nameField: "Synthetic name", "script": "synthetic-private-script", "profile": "synthetic-private-xml", "file_url": "https://synthetic.invalid/?secret=private", "enrollment_code": map[string]string{"code": "654321"}}
				var body any = []any{item}
				if !array {
					body = map[string]any{"results": body, "next": nil}
				}
				bytes, _ := json.Marshal(body)
				return response(200, string(bytes)), nil
			})
			items, err := c.ListInventory(context.Background(), kind)
			if err != nil || len(items) != 1 || items[0].ID != testID || calls != 1 {
				t.Fatalf("inventory: %v", err)
			}
			bytes, _ := json.Marshal(items)
			if strings.Contains(string(bytes), "private") || strings.Contains(string(bytes), "654321") {
				t.Fatal("inventory retained nonidentity fields")
			}
		})
	}
	c := testClient(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("unknown inventory kind reached network")
		return nil, nil
	})
	if _, err := c.ListInventory(context.Background(), "../devices"); err == nil {
		t.Fatal("accepted arbitrary inventory endpoint")
	}
}

func TestInventoryPaginationMustComplete(t *testing.T) {
	calls := 0
	c := testClient(t, func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.RawQuery == "" {
			return response(200, `{"next":"?offset=1","results":[{"id":"`+testID+`","name":"Synthetic"}]}`), nil
		}
		return response(200, `{"next":null,"results":[{"id":"`+testCategory+`","name":"Synthetic second"}]}`), nil
	})
	items, err := c.ListInventory(context.Background(), "custom_scripts")
	if err != nil || len(items) != 2 || calls != 2 {
		t.Fatal("inventory did not read every page")
	}
	for _, body := range []string{`{}`, `{"next":"https://evil.example/api/v1/library/custom-scripts?offset=1","results":[]}`, `{"next":"/api/v1/devices?offset=1","results":[]}`, `{"next":"?offset=1","results":[]}`, `{"next":null,"results":[{"id":"` + testID + `","name":"a"},{"id":"` + testID + `","name":"b"}]}`} {
		calls = 0
		c := testClient(t, func(*http.Request) (*http.Response, error) { calls++; return response(200, body), nil })
		if _, err := c.ListInventory(context.Background(), "custom_scripts"); err == nil || calls > 2 {
			t.Fatal("unsafe or incomplete inventory accepted")
		}
	}
}
