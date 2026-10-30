// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const adeRoutingFixture = `{"id":"11111111-2222-4333-8444-555555555555","blueprint_id":null,"use_blueprint_routing":true,"defaults":{"phone":"1234567890","email":"synthetic@example.invalid"},"server_name":"Synthetic Apple Server","access_token_expiry":"2027-10-29T00:00:00Z"}`

func TestADEAssignmentReadContracts(t *testing.T) {
	for name, fixture := range map[string]string{
		"legacy nested Blueprint":   adeFixture,
		"current fixed Blueprint":   strings.Replace(strings.Replace(adeRoutingFixture, `"blueprint_id":null`, `"blueprint_id":"`+testCategory+`"`, 1), `"use_blueprint_routing":true`, `"use_blueprint_routing":false`, 1),
		"current Blueprint Routing": adeRoutingFixture,
	} {
		t.Run(name, func(t *testing.T) {
			value, err := decodeADE([]byte(fixture), testID)
			if err != nil {
				t.Fatal(err)
			}
			if name == "current Blueprint Routing" {
				if !value.UseBlueprintRouting || value.BlueprintID != nil || value.Blueprint.ID != "" {
					t.Fatal("routing must retain an explicitly absent Blueprint")
				}
			} else if value.UseBlueprintRouting || value.BlueprintID == nil || *value.BlueprintID != testCategory {
				t.Fatal("fixed Blueprint assignment was not preserved")
			}
		})
	}
	for name, fixture := range map[string]string{
		"missing routing":                    strings.Replace(adeRoutingFixture, `"use_blueprint_routing":true,`, "", 1),
		"missing Blueprint":                  strings.Replace(adeRoutingFixture, `"blueprint_id":null,`, "", 1),
		"null routing":                       strings.Replace(adeRoutingFixture, `"use_blueprint_routing":true`, `"use_blueprint_routing":null`, 1),
		"invalid routing":                    strings.Replace(adeRoutingFixture, `"use_blueprint_routing":true`, `"use_blueprint_routing":"true"`, 1),
		"disabled routing without Blueprint": strings.Replace(adeRoutingFixture, `"use_blueprint_routing":true`, `"use_blueprint_routing":false`, 1),
		"routing with Blueprint":             strings.Replace(adeRoutingFixture, `"blueprint_id":null`, `"blueprint_id":"`+testCategory+`"`, 1),
		"contradictory nested Blueprint":     strings.Replace(adeRoutingFixture, `"blueprint_id":null`, `"blueprint":{"id":"`+testCategory+`"},"blueprint_id":null`, 1),
		"unknown nested-null assignment":     strings.Replace(adeFixture, `"blueprint":{"id":"`+testCategory+`"}`, `"blueprint":null`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			value, err := decodeADE([]byte(fixture), "")
			if err == nil || value.ID != testID {
				t.Fatal("unobservable assignment accepted or partial creation identity lost")
			}
		})
	}
}

func TestADERoutingWireAndInvalidCoupling(t *testing.T) {
	input := ADEInput{UseBlueprintRouting: true, Phone: "1234567890", Email: "synthetic@example.invalid"}
	calls := 0
	c := testClient(t, func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Method == http.MethodPost {
			if req.ParseMultipartForm(1024*1024) != nil {
				t.Fatal("invalid multipart")
			}
			defer req.MultipartForm.RemoveAll()
			if v, exists := req.MultipartForm.Value["blueprint_id"]; !exists || len(v) != 1 || v[0] != "" || req.FormValue("use_blueprint_routing") != "true" {
				t.Fatal("routing multipart must explicitly pair true with an empty Blueprint field")
			}
		} else if req.Method == http.MethodPatch {
			var fields map[string]any
			if json.NewDecoder(req.Body).Decode(&fields) != nil || fields["blueprint_id"] != nil || fields["use_blueprint_routing"] != true || len(fields) != 4 {
				t.Fatal("routing JSON must explicitly pair true with null Blueprint")
			}
		}
		return response(200, adeRoutingFixture), nil
	})
	ctx := context.Background()
	if _, err := c.CreateADEIntegration(ctx, input, []byte("synthetic-token")); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UpdateADEIntegration(ctx, testID, input); err != nil {
		t.Fatal(err)
	}
	if _, err := c.RenewADEIntegration(ctx, testID, input, []byte("synthetic-token")); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []ADEInput{
		{BlueprintID: testCategory, UseBlueprintRouting: true, Phone: input.Phone, Email: input.Email},
		{Phone: input.Phone, Email: input.Email},
	} {
		if _, err := c.UpdateADEIntegration(ctx, testID, invalid); err == nil {
			t.Fatal("invalid assignment reached API")
		}
	}
	if calls != 3 {
		t.Fatal("unexpected writes or rejected coupling reached transport")
	}
}
