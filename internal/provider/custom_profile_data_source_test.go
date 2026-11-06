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

const profileLookupID = "aaaaaaaa-bbbb-4ccc-8ddd-000000000091"

const lookupProfileXML = "\n<plist version=\"1.0\"><dict><key>PayloadDescription</key><string>  synthetic-private-profile  </string></dict></plist>\n"

func lookupProfileFixture() client.Profile {
	return client.Profile{ID: profileLookupID, MDMIdentifier: "com.example.synthetic", ProfileInput: client.ProfileInput{Name: "Synthetic profile", Profile: lookupProfileXML, RunsOnMac: true}}
}

func profileLookupConfig(t *testing.T, id types.String) tfsdk.State {
	t.Helper()
	ctx := context.Background()
	var s datasource.SchemaResponse
	(&CustomProfileDataSource{}).Schema(ctx, datasource.SchemaRequest{}, &s)
	if diags := s.Schema.ValidateImplementation(ctx); diags.HasError() {
		t.Fatal(diags)
	}
	state := tfsdk.State{Schema: s.Schema, Raw: tftypes.NewValue(s.Schema.Type().TerraformType(ctx), nil)}
	data := profileLookupModel{LibraryItemID: id, ID: types.StringNull(), Name: types.StringNull(), Profile: types.StringNull(), MDMIdentifier: types.StringNull(), Active: types.BoolNull(), RunsOnMac: types.BoolNull(), RunsOnIphone: types.BoolNull(), RunsOnIpad: types.BoolNull(), RunsOnTV: types.BoolNull(), RunsOnVision: types.BoolNull()}
	if diags := state.Set(ctx, &data); diags.HasError() {
		t.Fatal(diags)
	}
	return state
}

func profileLookupClient(t *testing.T, request func(*http.Request) (*http.Response, error)) *client.Client {
	t.Helper()
	c, err := client.New("https://synthetic.api.iru.com", "synthetic-token", &http.Client{Transport: syntheticTransport(request)})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCustomProfileLookupSchemaAndObservedValues(t *testing.T) {
	ctx := context.Background()
	state := profileLookupConfig(t, types.StringValue(strings.ToUpper(profileLookupID)))
	model := lookupProfileFixture()
	body, _ := json.Marshal(model)
	// A single-object read must not follow an unrelated pagination hint or expose
	// fields outside its documented configuration projection.
	var fields map[string]any
	_ = json.Unmarshal(body, &fields)
	fields["next"], fields["private_extra"] = "https://other.invalid/collect", "synthetic-excluded-value"
	body, _ = json.Marshal(fields)
	reads := 0
	d := &CustomProfileDataSource{client: profileLookupClient(t, func(req *http.Request) (*http.Response, error) {
		reads++
		if req.Method != http.MethodGet || req.URL.Path != "/api/v1/library/custom-profiles/"+profileLookupID || req.URL.RawQuery != "" {
			t.Fatal("configuration lookup mutated or enumerated a collection")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})}
	resp := datasource.ReadResponse{State: state}
	d.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: state.Schema, Raw: state.Raw}}, &resp)
	var got profileLookupModel
	if diags := resp.State.Get(ctx, &got); diags.HasError() || resp.Diagnostics.HasError() {
		t.Fatal("configuration read failed")
	}
	if reads != 1 || got.LibraryItemID.ValueString() != strings.ToUpper(profileLookupID) || got.ID.ValueString() != profileLookupID || got.Profile.ValueString() != lookupProfileXML || got.MDMIdentifier.ValueString() != model.MDMIdentifier || got.Name.ValueString() != model.Name || !got.RunsOnMac.ValueBool() {
		t.Fatal("configuration projection changed identity, content or flags")
	}
	for _, flag := range []types.Bool{got.Active, got.RunsOnIphone, got.RunsOnIpad, got.RunsOnTV, got.RunsOnVision} {
		if flag.IsNull() || flag.IsUnknown() || flag.ValueBool() {
			t.Fatal("an explicit remote false was not preserved as a known false")
		}
	}
	var schemaResponse datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &schemaResponse)
	if len(schemaResponse.Schema.Attributes) != 11 || !schemaResponse.Schema.Attributes["profile"].IsSensitive() || !schemaResponse.Schema.Attributes["library_item_id"].IsRequired() {
		t.Fatal("lookup schema exposes unexpected configuration or lacks sensitive XML")
	}
	for name, attr := range schemaResponse.Schema.Attributes {
		if name != "library_item_id" && (!attr.IsComputed() || attr.IsOptional() || attr.IsRequired()) {
			t.Fatal("a remote output is configurable instead of computed")
		}
	}
}

func TestCustomProfileLookupRejectsMissingNullAndFailureWithoutInventedState(t *testing.T) {
	ctx := context.Background()
	base := profileLookupConfig(t, types.StringValue(profileLookupID))
	config := tfsdk.Config{Schema: base.Schema, Raw: base.Raw}
	encoded, _ := json.Marshal(lookupProfileFixture())
	seed := &CustomProfileDataSource{client: profileLookupClient(t, func(*http.Request) (*http.Response, error) {
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
		{"wrong-identity", strings.Replace(string(encoded), profileLookupID, syntheticCategory, 1), 200},
		{"invalid-xml", strings.Replace(string(encoded), `"profile":`+string(mustJSON(t, lookupProfileXML)), `"profile":"synthetic-private-xml"`, 1), 200},
		{"response-bound", strings.Repeat("x", 2*1024*1024+1), 200},
	}
	for _, field := range []string{"id", "name", "profile", "mdm_identifier", "active", "runs_on_mac", "runs_on_iphone", "runs_on_ipad", "runs_on_tv", "runs_on_vision"} {
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
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reads := 0
			d := &CustomProfileDataSource{client: profileLookupClient(t, func(req *http.Request) (*http.Response, error) {
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
			for _, private := range []string{"synthetic-token", "synthetic-private", "<plist"} {
				if strings.Contains(fmt.Sprint(resp.Diagnostics), private) {
					t.Fatal("private response/configuration reached diagnostics")
				}
			}
		})
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestCustomProfileLookupInputAndConfigureGuards(t *testing.T) {
	ctx := context.Background()
	d := &CustomProfileDataSource{client: profileLookupClient(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("unknown/null/invalid input reached transport")
		return nil, nil
	})}
	for _, id := range []types.String{types.StringUnknown(), types.StringNull(), types.StringValue("partial-profile-name")} {
		state := profileLookupConfig(t, id)
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

func TestCustomProfileLookupTransientGETRecovery(t *testing.T) {
	ctx := context.Background()
	state := profileLookupConfig(t, types.StringValue(profileLookupID))
	reads := 0
	d := &CustomProfileDataSource{client: profileLookupClient(t, func(req *http.Request) (*http.Response, error) {
		reads++
		status, body := 503, "synthetic-private-response"
		if reads == 2 {
			status, body = 200, string(mustJSON(t, lookupProfileFixture()))
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

func TestCustomProfileLookupPropagatesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	state := profileLookupConfig(t, types.StringValue(profileLookupID))
	d := &CustomProfileDataSource{client: profileLookupClient(t, func(req *http.Request) (*http.Response, error) {
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
