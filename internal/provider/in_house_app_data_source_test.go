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
	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
	"io"
	"net/http"
	"strings"
	"testing"
)

const inHouseLookupID = "aaaaaaaa-bbbb-4ccc-8ddd-000000000095"

func lookupInHouseFixture() client.InHouseApp {
	return client.InHouseApp{ID: inHouseLookupID, InHouseAppInput: client.InHouseAppInput{Name: "Synthetic IPA", FileKey: "synthetic-private/app.ipa"}}
}
func inHouseLookupConfig(t *testing.T, id types.String) tfsdk.State {
	t.Helper()
	ctx := context.Background()
	var s datasource.SchemaResponse
	(&InHouseAppDataSource{}).Schema(ctx, datasource.SchemaRequest{}, &s)
	if diags := s.Schema.ValidateImplementation(ctx); diags.HasError() {
		t.Fatal(diags)
	}
	state := tfsdk.State{Schema: s.Schema, Raw: tftypes.NewValue(s.Schema.Type().TerraformType(ctx), nil)}
	data := inHouseLookupModel{LibraryItemID: id, ID: types.StringNull(), Name: types.StringNull(), FileKey: types.StringNull(), Active: types.BoolNull(), RunsOnIphone: types.BoolNull(), RunsOnIpad: types.BoolNull(), RunsOnTV: types.BoolNull(), SHA256: types.StringNull(), FileSize: types.Int64Null()}
	if diags := state.Set(ctx, &data); diags.HasError() {
		t.Fatal(diags)
	}
	return state
}
func inHouseLookupClient(t *testing.T, request func(*http.Request) (*http.Response, error)) *client.Client {
	t.Helper()
	c, err := client.New("https://synthetic.api.iru.com", "synthetic-token", &http.Client{Transport: syntheticTransport(request)})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func TestInHouseAppLookupObservedValuesAndProjection(t *testing.T) {
	ctx := context.Background()
	state := inHouseLookupConfig(t, types.StringValue(strings.ToUpper(inHouseLookupID)))
	fixture := lookupInHouseFixture()
	values := map[string]any{}
	json.Unmarshal(mustJSON(t, fixture), &values)
	values["file_url"] = "https://other.invalid/synthetic-private-download"
	values["next"] = "https://other.invalid/synthetic-private-next"
	values["post_data"] = "synthetic-private-ticket"
	reads := 0
	d := &InHouseAppDataSource{client: inHouseLookupClient(t, func(r *http.Request) (*http.Response, error) {
		reads++
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/library/ipa-apps/"+inHouseLookupID || r.URL.RawQuery != "" {
			t.Fatal("lookup mutated, enumerated or followed a URL")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(mustJSON(t, values))))}, nil
	})}
	config := tfsdk.Config{Schema: state.Schema, Raw: state.Raw}
	read := func() inHouseLookupModel {
		t.Helper()
		resp := datasource.ReadResponse{State: state}
		d.Read(ctx, datasource.ReadRequest{Config: config}, &resp)
		var got inHouseLookupModel
		if diags := resp.State.Get(ctx, &got); diags.HasError() || resp.Diagnostics.HasError() {
			t.Fatal("lookup failed")
		}
		return got
	}
	got := read()
	if reads != 1 || got.ID.ValueString() != inHouseLookupID || got.LibraryItemID.ValueString() != strings.ToUpper(inHouseLookupID) || got.Name.ValueString() != fixture.Name || got.FileKey.ValueString() != fixture.FileKey || !got.SHA256.IsNull() || !got.FileSize.IsNull() {
		t.Fatal("identity, projection or null metadata changed")
	}
	for _, flag := range []types.Bool{got.Active, got.RunsOnIphone, got.RunsOnIpad, got.RunsOnTV} {
		if flag.IsNull() || flag.IsUnknown() || flag.ValueBool() {
			t.Fatal("explicit false did not remain false")
		}
	}
	sum := strings.Repeat("AB", 32)
	values["sha256"] = sum
	values["file_size"] = 0
	values["runs_on_tv"] = true
	got = read()
	if got.SHA256.ValueString() != sum || got.FileSize.IsNull() || got.FileSize.ValueInt64() != 0 || !got.RunsOnTV.ValueBool() {
		t.Fatal("reported case/zero/true values changed")
	}
	values["sha256"], values["file_size"] = nil, nil
	got = read()
	if !got.SHA256.IsNull() || !got.FileSize.IsNull() {
		t.Fatal("explicit null became a default")
	}
	var schemaResponse datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &schemaResponse)
	if len(schemaResponse.Schema.Attributes) != 10 || !schemaResponse.Schema.Attributes["file_key"].IsSensitive() {
		t.Fatal("unexpected schema projection or key sensitivity")
	}
	for name, attr := range schemaResponse.Schema.Attributes {
		if name == "library_item_id" {
			if !attr.IsRequired() {
				t.Fatal("selector is not required")
			}
		} else if !attr.IsComputed() || attr.IsOptional() || attr.IsRequired() {
			t.Fatal("observed output is configurable")
		}
	}
}
func TestInHouseAppLookupFailuresRetainPriorState(t *testing.T) {
	ctx := context.Background()
	base := inHouseLookupConfig(t, types.StringValue(inHouseLookupID))
	config := tfsdk.Config{Schema: base.Schema, Raw: base.Raw}
	encoded := mustJSON(t, lookupInHouseFixture())
	seed := &InHouseAppDataSource{client: inHouseLookupClient(t, func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(encoded)))}, nil
	})}
	prior := datasource.ReadResponse{State: base}
	seed.Read(ctx, datasource.ReadRequest{Config: config}, &prior)
	if prior.Diagnostics.HasError() {
		t.Fatal("seed lookup failed")
	}
	base = prior.State
	cases := []struct {
		name, body string
		status     int
	}{
		{"not-found", "synthetic-private-response", 404}, {"unauthorized", "synthetic-token", 401}, {"forbidden", "synthetic-private-response", 403}, {"transient-exhausted", "synthetic-private-response", 503}, {"rate-limited", "synthetic-private-response", 429}, {"bad-json", "synthetic-private-json", 200},
		{"wrong-identity", strings.Replace(string(encoded), inHouseLookupID, syntheticCategory, 1), 200}, {"response-bound", strings.Repeat("x", 2*1024*1024+1), 200},
	}
	for _, field := range []string{"id", "name", "file_key", "active", "runs_on_iphone", "runs_on_ipad", "runs_on_tv"} {
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
		"bad-key": func(v map[string]any) { v["file_key"] = "synthetic-private\nkey" }, "bad-checksum": func(v map[string]any) { v["sha256"] = "synthetic-private-checksum" }, "negative-size": func(v map[string]any) { v["file_size"] = -1 }, "wrong-bool": func(v map[string]any) { v["active"] = "false" }, "fractional-size": func(v map[string]any) { v["file_size"] = 1.5 },
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
			d := &InHouseAppDataSource{client: inHouseLookupClient(t, func(r *http.Request) (*http.Response, error) {
				reads++
				if r.Method != http.MethodGet {
					t.Fatal("failure path mutated")
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Retry-After": []string{"0"}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}
			resp := datasource.ReadResponse{State: base}
			d.Read(ctx, datasource.ReadRequest{Config: config}, &resp)
			if !resp.Diagnostics.HasError() || !resp.State.Raw.Equal(base.Raw) || reads < 1 || reads > 3 {
				t.Fatal("failure claimed success, changed prior state or escaped retry bounds")
			}
			if strings.Contains(fmt.Sprint(resp.Diagnostics), "synthetic-private") || strings.Contains(fmt.Sprint(resp.Diagnostics), "synthetic-token") {
				t.Fatal("private response reached diagnostics")
			}
		})
	}
}

func TestInHouseAppLookupInputAndConfigureGuards(t *testing.T) {
	ctx := context.Background()
	d := &InHouseAppDataSource{client: inHouseLookupClient(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("unknown/null/invalid input reached transport")
		return nil, nil
	})}
	for _, id := range []types.String{types.StringUnknown(), types.StringNull(), types.StringValue("partial-ipa-name")} {
		state := inHouseLookupConfig(t, id)
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

func TestInHouseAppLookupTransientGETRecovery(t *testing.T) {
	ctx := context.Background()
	state := inHouseLookupConfig(t, types.StringValue(inHouseLookupID))
	reads := 0
	d := &InHouseAppDataSource{client: inHouseLookupClient(t, func(req *http.Request) (*http.Response, error) {
		reads++
		status, body := 503, "synthetic-private-response"
		if reads == 2 {
			status, body = 200, string(mustJSON(t, lookupInHouseFixture()))
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

func TestInHouseAppLookupPropagatesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	state := inHouseLookupConfig(t, types.StringValue(inHouseLookupID))
	d := &InHouseAppDataSource{client: inHouseLookupClient(t, func(req *http.Request) (*http.Response, error) {
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
