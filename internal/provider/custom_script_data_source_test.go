// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
)

const scriptLookupID = "aaaaaaaa-bbbb-4ccc-8ddd-000000000092"
const lookupScriptText = "\n#!/bin/sh\nprintf '%s\\n' '  synthetic-private-script ☃  '\n\n"
const lookupRemediationText = "\t#!/bin/sh\nprintf '%s\\n' ' synthetic-private-remediation '  \r\n"

func lookupScriptFixture() client.ScriptConfiguration {
	return client.ScriptConfiguration{ID: scriptLookupID, Name: "Synthetic script", Script: lookupScriptText, RemediationScript: lookupRemediationText, ExecutionFrequency: "once"}
}

func scriptLookupConfig(t *testing.T, id types.String) tfsdk.State {
	t.Helper()
	ctx := context.Background()
	var s datasource.SchemaResponse
	(&CustomScriptDataSource{}).Schema(ctx, datasource.SchemaRequest{}, &s)
	if diags := s.Schema.ValidateImplementation(ctx); diags.HasError() {
		t.Fatal(diags)
	}
	state := tfsdk.State{Schema: s.Schema, Raw: tftypes.NewValue(s.Schema.Type().TerraformType(ctx), nil)}
	data := scriptLookupModel{LibraryItemID: id, ID: types.StringNull(), Name: types.StringNull(), Script: types.StringNull(), RemediationScript: types.StringNull(), ExecutionFrequency: types.StringNull(), Active: types.BoolNull(), Restart: types.BoolNull(), ShowInSelfService: types.BoolNull(), SelfServiceCategoryID: types.StringNull(), SelfServiceRecommended: types.BoolNull()}
	if diags := state.Set(ctx, &data); diags.HasError() {
		t.Fatal(diags)
	}
	return state
}

func scriptLookupClient(t *testing.T, request func(*http.Request) (*http.Response, error)) *client.Client {
	t.Helper()
	c, err := client.New("https://synthetic.api.iru.com", "synthetic-token", &http.Client{Transport: syntheticTransport(request)})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCustomScriptLookupSchemaAndObservedValues(t *testing.T) {
	ctx := context.Background()
	state := scriptLookupConfig(t, types.StringValue(strings.ToUpper(scriptLookupID)))
	model := lookupScriptFixture()
	body, _ := json.Marshal(model)
	// A single-object read must not follow an unrelated pagination hint or expose
	// fields outside its documented configuration projection.
	var fields map[string]any
	_ = json.Unmarshal(body, &fields)
	fields["next"], fields["private_extra"] = "https://other.invalid/collect", "synthetic-excluded-value"
	body, _ = json.Marshal(fields)
	reads := 0
	d := &CustomScriptDataSource{client: scriptLookupClient(t, func(req *http.Request) (*http.Response, error) {
		reads++
		if req.Method != http.MethodGet || req.URL.Path != "/api/v1/library/custom-scripts/"+scriptLookupID || req.URL.RawQuery != "" {
			t.Fatal("configuration lookup mutated or enumerated a collection")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})}
	resp := datasource.ReadResponse{State: state}
	d.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: state.Schema, Raw: state.Raw}}, &resp)
	var got scriptLookupModel
	if diags := resp.State.Get(ctx, &got); diags.HasError() || resp.Diagnostics.HasError() {
		t.Fatal("configuration read failed")
	}
	if reads != 1 || got.LibraryItemID.ValueString() != strings.ToUpper(scriptLookupID) || got.ID.ValueString() != scriptLookupID || got.Script.ValueString() != lookupScriptText || got.RemediationScript.ValueString() != lookupRemediationText || got.ExecutionFrequency.ValueString() != "once" || got.Name.ValueString() != model.Name {
		t.Fatal("configuration projection changed identity, content or flags")
	}
	for _, flag := range []types.Bool{got.Active, got.Restart} {
		if flag.IsNull() || flag.IsUnknown() || flag.ValueBool() {
			t.Fatal("an explicit remote false was not preserved as a known false")
		}
	}
	if !got.ShowInSelfService.IsNull() || !got.SelfServiceCategoryID.IsNull() || !got.SelfServiceRecommended.IsNull() {
		t.Fatal("absent metadata became false or an empty ID")
	}
	var schemaResponse datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &schemaResponse)
	if len(schemaResponse.Schema.Attributes) != 11 || (!schemaResponse.Schema.Attributes["script"].IsSensitive() || !schemaResponse.Schema.Attributes["remediation_script"].IsSensitive()) || !schemaResponse.Schema.Attributes["library_item_id"].IsRequired() {
		t.Fatal("lookup schema exposes unexpected configuration or lacks sensitive scripts")
	}
	for name, attr := range schemaResponse.Schema.Attributes {
		if name != "library_item_id" && (!attr.IsComputed() || attr.IsOptional() || attr.IsRequired()) {
			t.Fatal("a remote output is configurable instead of computed")
		}
	}
}

func TestCustomScriptLookupRejectsMissingNullAndFailureWithoutInventedState(t *testing.T) {
	ctx := context.Background()
	base := scriptLookupConfig(t, types.StringValue(scriptLookupID))
	config := tfsdk.Config{Schema: base.Schema, Raw: base.Raw}
	encoded, _ := json.Marshal(lookupScriptFixture())
	seed := &CustomScriptDataSource{client: scriptLookupClient(t, func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(encoded)))}, nil
	})}
	previous := datasource.ReadResponse{State: base}
	seed.Read(ctx, datasource.ReadRequest{Config: config}, &previous)
	if previous.Diagnostics.HasError() {
		t.Fatal("could not prepare previously observed configuration")
	}
	base = previous.State
	cases := []struct {
		name, body string
		status     int
	}{
		{"not-found", "synthetic-private-response", 404}, {"unauthorized", "synthetic-token", 401}, {"forbidden", "synthetic-private-response", 403},
		{"transient-exhausted", "synthetic-private-response", 503}, {"bad-json", "synthetic-private-json", 200},
		{"wrong-identity", strings.Replace(string(encoded), scriptLookupID, syntheticCategory, 1), 200},
		{"unsupported-frequency", strings.Replace(string(encoded), `"once"`, `"hourly"`, 1), 200},
		{"response-bound", strings.Repeat("x", 2*1024*1024+1), 200},
	}
	for _, field := range []string{"id", "name", "script", "remediation_script", "execution_frequency", "active", "restart"} {
		var values map[string]any
		_ = json.Unmarshal(encoded, &values)
		delete(values, field)
		body, _ := json.Marshal(values)
		cases = append(cases, struct {
			name, body string
			status     int
		}{"missing-" + field, string(body), 200})
		values[field] = nil
		body, _ = json.Marshal(values)
		cases = append(cases, struct {
			name, body string
			status     int
		}{"null-" + field, string(body), 200})
	}
	for _, field := range []string{"id", "name", "script", "remediation_script", "execution_frequency", "active", "restart", "show_in_self_service", "self_service_category_id", "self_service_recommended"} {
		var values map[string]any
		_ = json.Unmarshal(encoded, &values)
		values[field] = []any{"synthetic-private-invalid-type"}
		cases = append(cases, struct {
			name, body string
			status     int
		}{"type-" + field, string(mustJSON(t, values)), 200})
	}
	var invalidCategory map[string]any
	_ = json.Unmarshal(encoded, &invalidCategory)
	invalidCategory["self_service_category_id"] = "synthetic-private-invalid-id"
	cases = append(cases, struct {
		name, body string
		status     int
	}{"invalid-category", string(mustJSON(t, invalidCategory)), 200})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reads := 0
			d := &CustomScriptDataSource{client: scriptLookupClient(t, func(req *http.Request) (*http.Response, error) {
				reads++
				if req.Method != http.MethodGet {
					t.Fatal("error path attempted a mutation")
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Retry-After": []string{"0"}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}
			// Preserve the supplied cached state on failure; an error does not
			// certify that cached configuration is still current.
			resp := datasource.ReadResponse{State: base}
			d.Read(ctx, datasource.ReadRequest{Config: config}, &resp)
			if !resp.Diagnostics.HasError() || !resp.State.Raw.Equal(base.Raw) || reads < 1 || reads > 3 {
				t.Fatal("bad read invented successful state or escaped retry bounds")
			}
			for _, private := range []string{"synthetic-token", "synthetic-private", "#!/bin/sh"} {
				if strings.Contains(fmt.Sprint(resp.Diagnostics), private) {
					t.Fatal("private response/configuration reached diagnostics")
				}
			}
		})
	}
}

func TestCustomScriptLookupInputAndConfigureGuards(t *testing.T) {
	ctx := context.Background()
	d := &CustomScriptDataSource{client: scriptLookupClient(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("unknown/null/invalid input reached transport")
		return nil, nil
	})}
	for _, id := range []types.String{types.StringUnknown(), types.StringNull(), types.StringValue("partial-script-name")} {
		state := scriptLookupConfig(t, id)
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

func TestCustomScriptLookupTransientGETRecovery(t *testing.T) {
	ctx := context.Background()
	state := scriptLookupConfig(t, types.StringValue(scriptLookupID))
	reads := 0
	d := &CustomScriptDataSource{client: scriptLookupClient(t, func(req *http.Request) (*http.Response, error) {
		reads++
		status, body := 503, "synthetic-private-response"
		if reads == 2 {
			status, body = 200, string(mustJSON(t, lookupScriptFixture()))
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

func TestCustomScriptLookupPropagatesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	state := scriptLookupConfig(t, types.StringValue(scriptLookupID))
	d := &CustomScriptDataSource{client: scriptLookupClient(t, func(req *http.Request) (*http.Response, error) {
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

func TestCustomScriptLookupOptionalMetadataProjection(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"missing", "null", "false", "true", "hidden-metadata", "no-enforcement", "empty-remediation"} {
		t.Run(mode, func(t *testing.T) {
			state := scriptLookupConfig(t, types.StringValue(scriptLookupID))
			var fields map[string]any
			_ = json.Unmarshal(mustJSON(t, lookupScriptFixture()), &fields)
			delete(fields, "show_in_self_service")
			delete(fields, "self_service_category_id")
			delete(fields, "self_service_recommended")
			switch mode {
			case "null":
				fields["show_in_self_service"] = nil
				fields["self_service_category_id"] = nil
				fields["self_service_recommended"] = nil
			case "false":
				fields["show_in_self_service"] = false
				fields["self_service_recommended"] = false
			case "true":
				fields["show_in_self_service"] = true
				fields["self_service_category_id"] = syntheticCategory
				fields["self_service_recommended"] = true
			case "hidden-metadata":
				fields["show_in_self_service"] = false
				fields["self_service_category_id"] = syntheticCategory
				fields["self_service_recommended"] = true
			case "no-enforcement":
				fields["execution_frequency"] = "no_enforcement"
			case "empty-remediation":
				fields["remediation_script"] = ""
			}
			d := &CustomScriptDataSource{client: scriptLookupClient(t, func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(mustJSON(t, fields))))}, nil
			})}
			resp := datasource.ReadResponse{State: state}
			d.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: state.Schema, Raw: state.Raw}}, &resp)
			var got scriptLookupModel
			if resp.Diagnostics.HasError() || resp.State.Get(ctx, &got).HasError() {
				t.Fatal("observational lookup failed")
			}
			if got.Script.ValueString() != lookupScriptText {
				t.Fatal("meaningful whitespace changed")
			}
			if mode == "missing" || mode == "null" || mode == "no-enforcement" || mode == "empty-remediation" {
				if !got.ShowInSelfService.IsNull() || !got.SelfServiceCategoryID.IsNull() || !got.SelfServiceRecommended.IsNull() {
					t.Fatal("optional missing/null metadata lost")
				}
			} else {
				if got.ShowInSelfService.IsNull() || got.ShowInSelfService.ValueBool() != (mode == "true") || got.SelfServiceRecommended.IsNull() || got.SelfServiceRecommended.ValueBool() != (mode != "false") {
					t.Fatal("observed true/false metadata lost")
				}
				if mode != "false" && got.SelfServiceCategoryID.ValueString() != syntheticCategory {
					t.Fatal("observed category dropped while hidden")
				}
			}
			if mode == "empty-remediation" && (got.RemediationScript.IsNull() || got.RemediationScript.ValueString() != "") {
				t.Fatal("returned empty remediation became null")
			}
			if mode == "no-enforcement" && got.ExecutionFrequency.ValueString() != "no_enforcement" {
				t.Fatal("read applied managed-resource constraints")
			}
		})
	}
}
