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

const appLookupID = "aaaaaaaa-bbbb-4ccc-8ddd-000000000094"
const lookupAppScript = "\n#!/bin/sh\nprintf '%s\\n' '  synthetic-private-app ☃  '\r\n"
const lookupAppKey = "synthetic-private/installer.pkg"

func lookupAppFixture() client.AppConfiguration {
	return client.AppConfiguration{ID: appLookupID, Name: "Synthetic app", FileKey: lookupAppKey, InstallType: "package", InstallEnforcement: "install_once", AuditScript: lookupAppScript, PreinstallScript: lookupAppScript, PostinstallScript: lookupAppScript}
}

func appLookupConfig(t *testing.T, id types.String) tfsdk.State {
	t.Helper()
	ctx := context.Background()
	var s datasource.SchemaResponse
	(&CustomAppDataSource{}).Schema(ctx, datasource.SchemaRequest{}, &s)
	if diags := s.Schema.ValidateImplementation(ctx); diags.HasError() {
		t.Fatal(diags)
	}
	state := tfsdk.State{Schema: s.Schema, Raw: tftypes.NewValue(s.Schema.Type().TerraformType(ctx), nil)}
	data := appLookupModel{LibraryItemID: id, ID: types.StringNull(), Name: types.StringNull(), FileKey: types.StringNull(), InstallType: types.StringNull(), InstallEnforcement: types.StringNull(), AuditScript: types.StringNull(), PreinstallScript: types.StringNull(), PostinstallScript: types.StringNull(), Active: types.BoolNull(), Restart: types.BoolNull(), UnzipLocation: types.StringNull(), ShowInSelfService: types.BoolNull(), SelfServiceCategoryID: types.StringNull(), SelfServiceRecommended: types.BoolNull(), SHA256: types.StringNull(), FileSize: types.Int64Null()}
	if diags := state.Set(ctx, &data); diags.HasError() {
		t.Fatal(diags)
	}
	return state
}

func appLookupClient(t *testing.T, request func(*http.Request) (*http.Response, error)) *client.Client {
	t.Helper()
	c, err := client.New("https://synthetic.api.iru.com", "synthetic-token", &http.Client{Transport: syntheticTransport(request)})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCustomAppLookupSchemaAndObservedValues(t *testing.T) {
	ctx := context.Background()
	state := appLookupConfig(t, types.StringValue(strings.ToUpper(appLookupID)))
	model := lookupAppFixture()
	body, _ := json.Marshal(model)
	// A single-object read must not follow an unrelated pagination hint or expose
	// fields outside its documented configuration projection.
	var fields map[string]any
	_ = json.Unmarshal(body, &fields)
	fields["next"], fields["file_url"], fields["upload_credentials"] = "https://other.invalid/collect", "https://other.invalid/installer?token=synthetic-private-url", map[string]any{"secret": "synthetic-private-upload"}
	body, _ = json.Marshal(fields)
	reads := 0
	d := &CustomAppDataSource{client: appLookupClient(t, func(req *http.Request) (*http.Response, error) {
		reads++
		if req.Method != http.MethodGet || req.URL.Path != "/api/v1/library/custom-apps/"+appLookupID || req.URL.RawQuery != "" {
			t.Fatal("configuration lookup mutated or enumerated a collection")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})}
	resp := datasource.ReadResponse{State: state}
	d.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: state.Schema, Raw: state.Raw}}, &resp)
	var got appLookupModel
	if diags := resp.State.Get(ctx, &got); diags.HasError() || resp.Diagnostics.HasError() {
		t.Fatal("configuration read failed")
	}
	if reads != 1 || got.LibraryItemID.ValueString() != strings.ToUpper(appLookupID) || got.ID.ValueString() != appLookupID || got.FileKey.ValueString() != lookupAppKey || got.AuditScript.ValueString() != lookupAppScript || got.PreinstallScript.ValueString() != lookupAppScript || got.PostinstallScript.ValueString() != lookupAppScript || got.InstallType.ValueString() != "package" || got.InstallEnforcement.ValueString() != "install_once" || got.Name.ValueString() != model.Name {
		t.Fatal("configuration projection changed identity, bytes or fields")
	}
	for _, flag := range []types.Bool{got.Active, got.Restart} {
		if flag.IsNull() || flag.IsUnknown() || flag.ValueBool() {
			t.Fatal("an explicit remote false was not preserved as a known false")
		}
	}
	if !got.ShowInSelfService.IsNull() || !got.SelfServiceCategoryID.IsNull() || !got.SelfServiceRecommended.IsNull() || !got.UnzipLocation.IsNull() || !got.SHA256.IsNull() || !got.FileSize.IsNull() {
		t.Fatal("absent metadata became false or an empty ID")
	}
	var schemaResponse datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &schemaResponse)
	if len(schemaResponse.Schema.Attributes) != 17 || !schemaResponse.Schema.Attributes["library_item_id"].IsRequired() {
		t.Fatal("unexpected lookup schema")
	}
	for _, key := range []string{"file_key", "audit_script", "preinstall_script", "postinstall_script"} {
		if !schemaResponse.Schema.Attributes[key].IsSensitive() {
			t.Fatal("private field lacks sensitivity")
		}
	}
	for _, key := range []string{"file_url", "upload_credentials", "source_file", "source_sha256"} {
		if _, present := schemaResponse.Schema.Attributes[key]; present {
			t.Fatal("download/upload/local source field exposed")
		}
	}
	for name, attr := range schemaResponse.Schema.Attributes {
		if name != "library_item_id" && (!attr.IsComputed() || attr.IsOptional() || attr.IsRequired()) {
			t.Fatal("a remote output is configurable instead of computed")
		}
	}
}

func TestCustomAppLookupRejectsMissingNullAndFailureWithoutInventedState(t *testing.T) {
	ctx := context.Background()
	base := appLookupConfig(t, types.StringValue(appLookupID))
	config := tfsdk.Config{Schema: base.Schema, Raw: base.Raw}
	encoded, _ := json.Marshal(lookupAppFixture())
	seed := &CustomAppDataSource{client: appLookupClient(t, func(*http.Request) (*http.Response, error) {
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
		{"wrong-identity", strings.Replace(string(encoded), appLookupID, syntheticCategory, 1), 200},
		{"unsupported-enforcement", strings.Replace(string(encoded), `"install_once"`, `"hourly"`, 1), 200},
		{"response-bound", strings.Repeat("x", 2*1024*1024+1), 200},
	}
	for _, field := range []string{"id", "name", "file_key", "install_type", "install_enforcement", "audit_script", "preinstall_script", "postinstall_script", "active", "restart"} {
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
	for _, field := range []string{"id", "name", "file_key", "install_type", "install_enforcement", "audit_script", "preinstall_script", "postinstall_script", "active", "restart", "unzip_location", "show_in_self_service", "self_service_category_id", "self_service_recommended", "sha256", "file_size"} {
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
	for label, change := range map[string]func(map[string]any){
		"bad-key":          func(v map[string]any) { v["file_key"] = "synthetic-private\nkey" },
		"empty-key":        func(v map[string]any) { v["file_key"] = "" },
		"bad-install-type": func(v map[string]any) { v["install_type"] = "unsupported" },
		"bad-sha":          func(v map[string]any) { v["sha256"] = "synthetic-private-sha" },
		"negative-size":    func(v map[string]any) { v["file_size"] = -1 },
		"fractional-size":  func(v map[string]any) { v["file_size"] = 1.5 },
	} {
		var v map[string]any
		_ = json.Unmarshal(encoded, &v)
		change(v)
		cases = append(cases, struct {
			name, body string
			status     int
		}{label, string(mustJSON(t, v)), 200})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reads := 0
			d := &CustomAppDataSource{client: appLookupClient(t, func(req *http.Request) (*http.Response, error) {
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

func TestCustomAppLookupInputAndConfigureGuards(t *testing.T) {
	ctx := context.Background()
	d := &CustomAppDataSource{client: appLookupClient(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("unknown/null/invalid input reached transport")
		return nil, nil
	})}
	for _, id := range []types.String{types.StringUnknown(), types.StringNull(), types.StringValue("partial-app-name")} {
		state := appLookupConfig(t, id)
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

func TestCustomAppLookupTransientGETRecovery(t *testing.T) {
	ctx := context.Background()
	state := appLookupConfig(t, types.StringValue(appLookupID))
	reads := 0
	d := &CustomAppDataSource{client: appLookupClient(t, func(req *http.Request) (*http.Response, error) {
		reads++
		status, body := 503, "synthetic-private-response"
		if reads == 2 {
			status, body = 200, string(mustJSON(t, lookupAppFixture()))
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

func TestCustomAppLookupPropagatesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	state := appLookupConfig(t, types.StringValue(appLookupID))
	d := &CustomAppDataSource{client: appLookupClient(t, func(req *http.Request) (*http.Response, error) {
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

func TestCustomAppLookupOptionalMetadataProjection(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"missing", "null", "false-zero-empty", "true", "hidden-metadata", "no-enforcement", "zip", "image"} {
		t.Run(mode, func(t *testing.T) {
			state := appLookupConfig(t, types.StringValue(appLookupID))
			var fields map[string]any
			_ = json.Unmarshal(mustJSON(t, lookupAppFixture()), &fields)
			optional := []string{"unzip_location", "show_in_self_service", "self_service_category_id", "self_service_recommended", "sha256", "file_size"}
			for _, k := range optional {
				delete(fields, k)
			}
			switch mode {
			case "null":
				for _, k := range optional {
					fields[k] = nil
				}
			case "false-zero-empty":
				fields["show_in_self_service"] = false
				fields["self_service_recommended"] = false
				fields["unzip_location"] = ""
				fields["file_size"] = int64(0)
				fields["audit_script"] = ""
				fields["preinstall_script"] = ""
				fields["postinstall_script"] = ""
			case "true", "hidden-metadata":
				fields["show_in_self_service"] = mode == "true"
				fields["self_service_recommended"] = true
				fields["self_service_category_id"] = syntheticCategory
				fields["sha256"] = strings.Repeat("A", 64)
				fields["file_size"] = int64(1234)
				fields["unzip_location"] = "/Applications/Synthetic"
			case "no-enforcement":
				fields["install_enforcement"] = "no_enforcement"
			case "zip":
				fields["install_type"] = "zip" // observe missing extraction metadata without write constraints
			case "image":
				fields["install_type"] = "image"
			}
			d := &CustomAppDataSource{client: appLookupClient(t, func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(mustJSON(t, fields))))}, nil
			})}
			resp := datasource.ReadResponse{State: state}
			d.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: state.Schema, Raw: state.Raw}}, &resp)
			var got appLookupModel
			if resp.Diagnostics.HasError() || resp.State.Get(ctx, &got).HasError() {
				t.Fatal("observational lookup failed")
			}
			if mode == "true" || mode == "hidden-metadata" {
				if got.ShowInSelfService.IsNull() || got.ShowInSelfService.ValueBool() != (mode == "true") || !got.SelfServiceRecommended.ValueBool() || got.SelfServiceCategoryID.ValueString() != syntheticCategory || got.SHA256.ValueString() != strings.Repeat("A", 64) || got.FileSize.ValueInt64() != 1234 || got.UnzipLocation.ValueString() != "/Applications/Synthetic" {
					t.Fatal("observed metadata was dropped or normalized")
				}
			} else if mode == "false-zero-empty" {
				if got.ShowInSelfService.IsNull() || got.ShowInSelfService.ValueBool() || got.SelfServiceRecommended.IsNull() || got.SelfServiceRecommended.ValueBool() || got.FileSize.IsNull() || got.FileSize.ValueInt64() != 0 || got.UnzipLocation.IsNull() || got.UnzipLocation.ValueString() != "" || got.AuditScript.IsNull() || got.AuditScript.ValueString() != "" || got.PreinstallScript.ValueString() != "" || got.PostinstallScript.ValueString() != "" {
					t.Fatal("false, zero or empty became null")
				}
			} else if !got.ShowInSelfService.IsNull() || !got.SelfServiceCategoryID.IsNull() || !got.SelfServiceRecommended.IsNull() || !got.UnzipLocation.IsNull() || !got.SHA256.IsNull() || !got.FileSize.IsNull() {
				t.Fatal("missing/null metadata invented")
			}
			if mode != "false-zero-empty" && (got.AuditScript.ValueString() != lookupAppScript || got.PreinstallScript.ValueString() != lookupAppScript || got.PostinstallScript.ValueString() != lookupAppScript) {
				t.Fatal("script bytes changed")
			}
		})
	}
}
