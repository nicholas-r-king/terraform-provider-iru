// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"io"
	"net/http"
	"strings"
	"testing"
)

const adeLookupID = "aaaaaaaa-bbbb-4ccc-8ddd-000000000096"

func lookupADEFixture() map[string]any {
	return map[string]any{"id": adeLookupID, "blueprint_id": strings.ToUpper(syntheticCategory), "use_blueprint_routing": false, "defaults": map[string]any{"email": "  synthetic-private@example.invalid  ", "phone": "  synthetic-private-phone  "}, "server_name": "Synthetic Apple Server", "access_token_expiry": "2027-11-06T00:00:00Z"}
}
func adeLookupConfig(t *testing.T, id types.String) tfsdk.State {
	t.Helper()
	ctx := context.Background()
	var s datasource.SchemaResponse
	(&ADEIntegrationDataSource{}).Schema(ctx, datasource.SchemaRequest{}, &s)
	if diags := s.Schema.ValidateImplementation(ctx); diags.HasError() {
		t.Fatal(diags)
	}
	state := tfsdk.State{Schema: s.Schema, Raw: tftypes.NewValue(s.Schema.Type().TerraformType(ctx), nil)}
	value := adeLookupModel{IntegrationID: id, ID: types.StringNull(), BlueprintID: types.StringNull(), UseBlueprintRouting: types.BoolNull(), Email: types.StringNull(), Phone: types.StringNull(), ServerName: types.StringNull(), AccessTokenExpiry: types.StringNull()}
	if diags := state.Set(ctx, &value); diags.HasError() {
		t.Fatal(diags)
	}
	return state
}
func TestADEIntegrationLookupObservedAssignmentAndSecretsExcluded(t *testing.T) {
	ctx := context.Background()
	state := adeLookupConfig(t, types.StringValue(strings.ToUpper(adeLookupID)))
	fixture := lookupADEFixture()
	fixture["server_token"] = "synthetic-private-server-token"
	fixture["access_token"] = "synthetic-private-access-token"
	fixture["server_token_version"] = 999
	fixture["next"] = "https://other.invalid/synthetic-private-next"
	fixture["org_email"] = "synthetic-private-org-email"
	reads := 0
	d := &ADEIntegrationDataSource{client: inHouseLookupClient(t, func(r *http.Request) (*http.Response, error) {
		reads++
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/integrations/apple/ade/"+adeLookupID || r.URL.RawQuery != "" {
			t.Fatal("lookup mutated, enumerated or followed a URL")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(mustJSON(t, fixture))))}, nil
	})}
	config := tfsdk.Config{Schema: state.Schema, Raw: state.Raw}
	read := func() adeLookupModel {
		t.Helper()
		resp := datasource.ReadResponse{State: state}
		d.Read(ctx, datasource.ReadRequest{Config: config}, &resp)
		var got adeLookupModel
		if diags := resp.State.Get(ctx, &got); diags.HasError() || resp.Diagnostics.HasError() {
			t.Fatal("lookup failed")
		}
		return got
	}
	got := read()
	if reads != 1 || got.IntegrationID.ValueString() != strings.ToUpper(adeLookupID) || got.ID.ValueString() != adeLookupID || got.BlueprintID.ValueString() != syntheticCategory || got.UseBlueprintRouting.IsNull() || got.UseBlueprintRouting.ValueBool() || got.Email.ValueString() != fixture["defaults"].(map[string]any)["email"] || got.Phone.ValueString() != fixture["defaults"].(map[string]any)["phone"] {
		t.Fatal("identity, fixed assignment or exact observed contact changed")
	}
	if got.ServerName.ValueString() != fixture["server_name"] || got.AccessTokenExpiry.ValueString() != fixture["access_token_expiry"] {
		t.Fatal("observed server metadata changed")
	}
	delete(fixture, "blueprint_id")
	delete(fixture, "use_blueprint_routing")
	fixture["blueprint"] = map[string]any{"id": strings.ToUpper(syntheticCategory)}
	got = read()
	if got.BlueprintID.ValueString() != syntheticCategory || got.UseBlueprintRouting.ValueBool() {
		t.Fatal("legacy fixed assignment was not decoded")
	}
	delete(fixture, "blueprint")
	fixture["blueprint_id"], fixture["use_blueprint_routing"] = nil, true
	fixture["defaults"] = map[string]any{"email": "", "phone": ""}
	got = read()
	if !got.BlueprintID.IsNull() || !got.UseBlueprintRouting.ValueBool() || got.Email.IsNull() || got.Email.ValueString() != "" || got.Phone.IsNull() || got.Phone.ValueString() != "" {
		t.Fatal("routing/null Blueprint or explicit empty contacts became defaults")
	}
	var schemaResponse datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &schemaResponse)
	if len(schemaResponse.Schema.Attributes) != 8 || !schemaResponse.Schema.Attributes["email"].IsSensitive() || !schemaResponse.Schema.Attributes["phone"].IsSensitive() {
		t.Fatal("schema leaked token/version fields or lost contact sensitivity")
	}
	for name, attr := range schemaResponse.Schema.Attributes {
		if name == "integration_id" {
			if !attr.IsRequired() {
				t.Fatal("selector is not required")
			}
		} else if !attr.IsComputed() || attr.IsOptional() || attr.IsRequired() {
			t.Fatal("remote observation is configurable")
		}
	}
}
func TestADEIntegrationLookupFailuresPreserveState(t *testing.T) {
	ctx := context.Background()
	base := adeLookupConfig(t, types.StringValue(adeLookupID))
	config := tfsdk.Config{Schema: base.Schema, Raw: base.Raw}
	encoded := mustJSON(t, lookupADEFixture())
	seed := &ADEIntegrationDataSource{client: inHouseLookupClient(t, func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(encoded)))}, nil
	})}
	prior := datasource.ReadResponse{State: base}
	seed.Read(ctx, datasource.ReadRequest{Config: config}, &prior)
	if prior.Diagnostics.HasError() {
		t.Fatal("seed failed")
	}
	base = prior.State
	cases := []struct {
		name, body string
		status     int
	}{
		{"not-found", "synthetic-private", 404}, {"unauthorized", "synthetic-token", 401}, {"forbidden", "synthetic-private", 403}, {"transient-exhausted", "synthetic-private", 503}, {"rate-limited", "synthetic-private", 429}, {"malformed", "synthetic-private", 200}, {"wrong-id", strings.Replace(string(encoded), adeLookupID, syntheticCategory, 1), 200}, {"response-bound", strings.Repeat("x", 2*1024*1024+1), 200},
	}
	for _, field := range []string{"id", "defaults", "server_name", "access_token_expiry", "blueprint_id"} {
		var values map[string]any
		json.Unmarshal(encoded, &values)
		delete(values, field)
		cases = append(cases, struct {
			name, body string
			status     int
		}{"missing-" + field, string(mustJSON(t, values)), 200})
		values[field] = nil
		cases = append(cases, struct {
			name, body string
			status     int
		}{"null-" + field, string(mustJSON(t, values)), 200})
	}
	for label, change := range map[string]func(map[string]any){
		"missing-email": func(v map[string]any) { delete(v["defaults"].(map[string]any), "email") }, "null-email": func(v map[string]any) { v["defaults"].(map[string]any)["email"] = nil }, "missing-phone": func(v map[string]any) { delete(v["defaults"].(map[string]any), "phone") }, "null-phone": func(v map[string]any) { v["defaults"].(map[string]any)["phone"] = nil }, "wrong-contact-type": func(v map[string]any) { v["defaults"].(map[string]any)["phone"] = 42 },
		"wrong-routing-type": func(v map[string]any) { v["use_blueprint_routing"] = "false" }, "null-routing": func(v map[string]any) { v["use_blueprint_routing"] = nil }, "routing-with-blueprint": func(v map[string]any) { v["use_blueprint_routing"] = true }, "conflicting-legacy-blueprint": func(v map[string]any) { v["blueprint"] = map[string]any{"id": assignmentItemA} }, "invalid-blueprint": func(v map[string]any) { v["blueprint_id"] = "synthetic-private-invalid" }, "routing-with-missing-blueprint": func(v map[string]any) { delete(v, "blueprint_id"); v["use_blueprint_routing"] = true }, "unknown-assignment": func(v map[string]any) { delete(v, "blueprint_id"); delete(v, "use_blueprint_routing") },
	} {
		var v map[string]any
		json.Unmarshal(encoded, &v)
		change(v)
		cases = append(cases, struct {
			name, body string
			status     int
		}{label, string(mustJSON(t, v)), 200})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reads := 0
			d := &ADEIntegrationDataSource{client: inHouseLookupClient(t, func(r *http.Request) (*http.Response, error) {
				reads++
				if r.Method != http.MethodGet {
					t.Fatal("failure path mutated")
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Retry-After": []string{"0"}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}
			resp := datasource.ReadResponse{State: base}
			d.Read(ctx, datasource.ReadRequest{Config: config}, &resp)
			if !resp.Diagnostics.HasError() || !resp.State.Raw.Equal(base.Raw) || reads < 1 || reads > 3 {
				t.Fatal("bad read invented success, changed state or escaped retry bounds")
			}
			if strings.Contains(fmt.Sprint(resp.Diagnostics), "synthetic-private") || strings.Contains(fmt.Sprint(resp.Diagnostics), "synthetic-token") {
				t.Fatal("private fields leaked in diagnostics")
			}
		})
	}
}

func TestADEIntegrationLookupInputAndConfigureGuards(t *testing.T) {
	ctx := context.Background()
	d := &ADEIntegrationDataSource{client: inHouseLookupClient(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("unknown/null/invalid input reached transport")
		return nil, nil
	})}
	for _, id := range []types.String{types.StringUnknown(), types.StringNull(), types.StringValue("partial-integration-name")} {
		state := adeLookupConfig(t, id)
		config := tfsdk.Config{Schema: state.Schema, Raw: state.Raw}
		var validation datasource.ValidateConfigResponse
		d.ValidateConfig(ctx, datasource.ValidateConfigRequest{Config: config}, &validation)
		if id.IsUnknown() && validation.Diagnostics.HasError() {
			t.Fatal("validation prematurely rejected a deferred unknown")
		}
		if !id.IsUnknown() && !id.IsNull() && !validation.Diagnostics.HasError() {
			t.Fatal("known malformed selector was accepted")
		}
		resp := datasource.ReadResponse{State: state}
		d.Read(ctx, datasource.ReadRequest{Config: config}, &resp)
		if !resp.Diagnostics.HasError() || !resp.State.Raw.Equal(state.Raw) {
			t.Fatal("invalid direct read was not refused without changing state")
		}
	}
	var configured datasource.ConfigureResponse
	d.Configure(ctx, datasource.ConfigureRequest{ProviderData: "synthetic-private-wrong-type"}, &configured)
	if !configured.Diagnostics.HasError() || strings.Contains(fmt.Sprint(configured.Diagnostics), "synthetic-private") {
		t.Fatal("bad provider data was accepted or exposed")
	}
}

func TestADEIntegrationLookupTransientGETRecovery(t *testing.T) {
	ctx := context.Background()
	state := adeLookupConfig(t, types.StringValue(adeLookupID))
	reads := 0
	d := &ADEIntegrationDataSource{client: inHouseLookupClient(t, func(req *http.Request) (*http.Response, error) {
		reads++
		status, body := 503, "synthetic-private-response"
		if reads == 2 {
			status, body = 200, string(mustJSON(t, lookupADEFixture()))
		}
		if req.Method != http.MethodGet {
			t.Fatal("transient recovery attempted a write")
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Retry-After": []string{"0"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	resp := datasource.ReadResponse{State: state}
	d.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: state.Schema, Raw: state.Raw}}, &resp)
	if resp.Diagnostics.HasError() || reads != 2 {
		t.Fatal("bounded GET recovery failed")
	}
}

func TestADEIntegrationLookupPropagatesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	state := adeLookupConfig(t, types.StringValue(adeLookupID))
	d := &ADEIntegrationDataSource{client: inHouseLookupClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Context().Err() == nil {
			t.Fatal("read discarded parent cancellation")
		}
		return nil, req.Context().Err()
	})}
	resp := datasource.ReadResponse{State: state}
	d.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: state.Schema, Raw: state.Raw}}, &resp)
	if !resp.Diagnostics.HasError() || !resp.State.Raw.Equal(state.Raw) {
		t.Fatal("cancelled read changed state or claimed success")
	}
}
