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

	fwprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
)

const syntheticID = "a1111111-b222-4333-8444-555555555555"
const syntheticCategory = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"

type syntheticTransport func(*http.Request) (*http.Response, error)

func (f syntheticTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func scriptSchema(t *testing.T) schema.Schema {
	t.Helper()
	var resp resource.SchemaResponse
	(&CustomScriptResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	return resp.Schema
}

func stateFor(t *testing.T, data scriptModel) tfsdk.State {
	t.Helper()
	ctx := context.Background()
	s := scriptSchema(t)
	state := tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}
	if diags := state.Set(ctx, &data); diags.HasError() {
		t.Fatal(diags)
	}
	return state
}

func syntheticModel() scriptModel {
	return scriptModel{
		ID: types.StringValue(syntheticID), Name: types.StringValue("Synthetic audit"), Script: types.StringValue("#!/bin/sh\nexit 0\n"),
		RemediationScript: types.StringValue(""), ExecutionFrequency: types.StringValue("once"), Active: types.BoolValue(false),
		Restart: types.BoolValue(false), ShowInSelfService: types.BoolValue(false), SelfServiceCategoryID: types.StringNull(), SelfServiceRecommended: types.BoolValue(false),
	}
}

func mockedResource(t *testing.T, status int, body string, check func(*http.Request)) *CustomScriptResource {
	t.Helper()
	c, err := client.New("https://synthetic.api.iru.com", "synthetic-token", &http.Client{Transport: syntheticTransport(func(req *http.Request) (*http.Response, error) {
		if check != nil {
			check(req)
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	return &CustomScriptResource{client: c}
}

func TestScriptValidationDiagnosticsWithholdResponseContents(t *testing.T) {
	ctx := context.Background()
	state := stateFor(t, syntheticModel())
	calls := 0
	r := mockedResource(t, 400, `{"remediation_script":[{"code":"blank","message":"synthetic-token confidential response content"}]}`, func(*http.Request) { calls++ })
	resp := resource.CreateResponse{State: state}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: state.Schema, Raw: state.Raw}}, &resp)
	text := fmt.Sprint(resp.Diagnostics)
	if !resp.Diagnostics.HasError() || calls != 1 || !strings.Contains(text, "remediation_script=blank") ||
		strings.Contains(text, "synthetic-token") || strings.Contains(text, "confidential response content") {
		t.Fatal("resource did not preserve safe validation metadata or redacted content")
	}
}

func modelJSON(t *testing.T, model scriptModel) string {
	t.Helper()
	data, err := json.Marshal(client.Script{ID: syntheticID, ScriptInput: model.input()})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestResourceCRUDAndRepeatedRead(t *testing.T) {
	ctx := context.Background()
	model := syntheticModel()
	initial := stateFor(t, model)
	plan := tfsdk.Plan{Schema: initial.Schema, Raw: initial.Raw}
	created := resource.CreateResponse{State: initial}
	r := mockedResource(t, 201, modelJSON(t, model), func(req *http.Request) {
		if req.Method != "POST" {
			t.Error("wrong create method")
		}
	})
	r.Create(ctx, resource.CreateRequest{Plan: plan}, &created)
	if created.Diagnostics.HasError() || !created.State.Raw.Equal(initial.Raw) {
		t.Fatalf("create mapping failed: %v", created.Diagnostics)
	}
	read := resource.ReadResponse{State: created.State}
	r = mockedResource(t, 200, modelJSON(t, model), func(req *http.Request) {
		if req.Method != "GET" || !strings.HasSuffix(req.URL.Path, syntheticID) {
			t.Error("wrong refresh target")
		}
	})
	r.Read(ctx, resource.ReadRequest{State: created.State}, &read)
	if read.Diagnostics.HasError() || !read.State.Raw.Equal(created.State.Raw) {
		t.Fatalf("refresh changed matching state: %v", read.Diagnostics)
	}
	second := resource.ReadResponse{State: read.State}
	r.Read(ctx, resource.ReadRequest{State: read.State}, &second)
	if second.Diagnostics.HasError() || !second.State.Raw.Equal(read.State.Raw) {
		t.Fatal("repeated refresh is not stable")
	}
	model.Active = types.BoolValue(true)
	model.RemediationScript = types.StringValue("#!/bin/sh\nexit 0\n")
	updatedState := stateFor(t, model)
	updated := resource.UpdateResponse{State: read.State}
	r = mockedResource(t, 200, modelJSON(t, model), func(req *http.Request) {
		if req.Method != "PATCH" {
			t.Error("wrong update method")
		}
	})
	r.Update(ctx, resource.UpdateRequest{Plan: tfsdk.Plan{Schema: updatedState.Schema, Raw: updatedState.Raw}, State: read.State}, &updated)
	if updated.Diagnostics.HasError() || !updated.State.Raw.Equal(updatedState.Raw) {
		t.Fatalf("update failed: %v", updated.Diagnostics)
	}
	deleted := resource.DeleteResponse{State: updated.State}
	r = mockedResource(t, 204, "", func(req *http.Request) {
		if req.Method != "DELETE" {
			t.Error("wrong delete method")
		}
	})
	r.Delete(ctx, resource.DeleteRequest{State: updated.State}, &deleted)
	if deleted.Diagnostics.HasError() {
		t.Fatal(deleted.Diagnostics)
	}
	// Framework automatically removes state after a successful Delete return.
}

func TestImportThenReadReconstructsAllFields(t *testing.T) {
	ctx := context.Background()
	model := syntheticModel()
	model.ShowInSelfService = types.BoolValue(true)
	model.SelfServiceCategoryID = types.StringValue(syntheticCategory)
	model.SelfServiceRecommended = types.BoolValue(true)
	model.ExecutionFrequency = types.StringValue("no_enforcement")
	full := stateFor(t, model)
	imported := resource.ImportStateResponse{State: tfsdk.State{Schema: full.Schema, Raw: tftypes.NewValue(full.Schema.Type().TerraformType(ctx), nil)}}
	r := mockedResource(t, 200, modelJSON(t, model), nil)
	r.ImportState(ctx, resource.ImportStateRequest{ID: strings.ToUpper(syntheticID)}, &imported)
	if imported.Diagnostics.HasError() {
		t.Fatal(imported.Diagnostics)
	}
	read := resource.ReadResponse{State: imported.State}
	r.Read(ctx, resource.ReadRequest{State: imported.State}, &read)
	if read.Diagnostics.HasError() || !read.State.Raw.Equal(full.Raw) {
		t.Fatalf("import/read did not reconstruct state: %v", read.Diagnostics)
	}
	for _, id := range []string{"", "name", "../path", syntheticID + ",other"} {
		bad := resource.ImportStateResponse{State: full}
		r.ImportState(ctx, resource.ImportStateRequest{ID: id}, &bad)
		if !bad.Diagnostics.HasError() {
			t.Error("invalid import accepted")
		}
	}
}

func TestCategoryUUIDCasePreservesConfiguredState(t *testing.T) {
	ctx := context.Background()
	model := syntheticModel()
	model.ShowInSelfService = types.BoolValue(true)
	model.SelfServiceCategoryID = types.StringValue(strings.ToUpper(syntheticCategory))
	planned := stateFor(t, model)
	remote := model
	remote.SelfServiceCategoryID = types.StringValue(syntheticCategory)
	created := resource.CreateResponse{State: planned}
	r := mockedResource(t, 201, modelJSON(t, remote), nil)
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: planned.Schema, Raw: planned.Raw}}, &created)
	if created.Diagnostics.HasError() || !created.State.Raw.Equal(planned.Raw) {
		t.Fatalf("case-only UUID normalization changed configured state: %v", created.Diagnostics)
	}
	read := resource.ReadResponse{State: created.State}
	r = mockedResource(t, 200, modelJSON(t, remote), nil)
	r.Read(ctx, resource.ReadRequest{State: created.State}, &read)
	if read.Diagnostics.HasError() || !read.State.Raw.Equal(planned.Raw) {
		t.Fatal("case-only UUID refresh caused drift")
	}
	updated := resource.UpdateResponse{State: read.State}
	r.Update(ctx, resource.UpdateRequest{Plan: tfsdk.Plan{Schema: planned.Schema, Raw: planned.Raw}, State: read.State}, &updated)
	if updated.Diagnostics.HasError() || !updated.State.Raw.Equal(planned.Raw) {
		t.Fatal("case-only UUID update changed configured state")
	}
	remote.SelfServiceCategoryID = types.StringValue(syntheticID)
	r = mockedResource(t, 200, modelJSON(t, remote), nil)
	drift := resource.ReadResponse{State: read.State}
	r.Read(ctx, resource.ReadRequest{State: read.State}, &drift)
	var changed scriptModel
	if diags := drift.State.Get(ctx, &changed); diags.HasError() {
		t.Fatal(diags)
	}
	if drift.Diagnostics.HasError() || changed.SelfServiceCategoryID.ValueString() != syntheticID {
		t.Fatal("preserving UUID case hid a real category change")
	}
}

func TestNondefaultImportMakesDefaultChangesVisible(t *testing.T) {
	ctx := context.Background()
	remote := syntheticModel()
	remote.Active, remote.Restart, remote.ShowInSelfService, remote.SelfServiceRecommended = types.BoolValue(true), types.BoolValue(true), types.BoolValue(true), types.BoolValue(true)
	remote.ExecutionFrequency = types.StringValue("every_day")
	remote.RemediationScript = types.StringValue("#!/bin/sh\nexit 0\n")
	remote.SelfServiceCategoryID = types.StringValue(syntheticCategory)
	full := stateFor(t, remote)
	imported := resource.ImportStateResponse{State: tfsdk.State{Schema: full.Schema, Raw: tftypes.NewValue(full.Schema.Type().TerraformType(ctx), nil)}}
	reads := 0
	r := mockedResource(t, 200, modelJSON(t, remote), func(req *http.Request) {
		if req.Method != "GET" {
			t.Error("import/plan attempted a mutation")
		}
		reads++
	})
	r.ImportState(ctx, resource.ImportStateRequest{ID: syntheticID}, &imported)
	refreshed := resource.ReadResponse{State: imported.State}
	r.Read(ctx, resource.ReadRequest{State: imported.State}, &refreshed)
	if imported.Diagnostics.HasError() || refreshed.Diagnostics.HasError() || !refreshed.State.Raw.Equal(full.Raw) {
		t.Fatal("nondefault import lost remote settings")
	}
	server := providerserver.NewProtocol6(&protocolTestProvider{IruProvider: IruProvider{version: "test"}, c: r.client})()
	if _, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{}); err != nil {
		t.Fatal(err)
	}
	tfType := full.Schema.Type().TerraformType(ctx)
	prior, err := tfprotov6.NewDynamicValue(tfType, refreshed.State.Raw)
	if err != nil {
		t.Fatal(err)
	}
	plan := func(configModel, proposedModel scriptModel) tftypes.Value {
		t.Helper()
		cfgState, proposedState := stateFor(t, configModel), stateFor(t, proposedModel)
		cfg, err := tfprotov6.NewDynamicValue(tfType, cfgState.Raw)
		if err != nil {
			t.Fatal(err)
		}
		proposed, err := tfprotov6.NewDynamicValue(tfType, proposedState.Raw)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := server.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{TypeName: "iru_custom_script", PriorState: &prior, ProposedNewState: &proposed, Config: &cfg})
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range resp.Diagnostics {
			if d.Severity == tfprotov6.DiagnosticSeverityError {
				t.Fatalf("plan failed: %s", d.Detail)
			}
		}
		if resp.PlannedState == nil {
			t.Fatal("missing planned state")
		}
		value, err := resp.PlannedState.Unmarshal(tfType)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	matching := remote
	matching.ID = types.StringNull()
	if !plan(matching, remote).Equal(refreshed.State.Raw) {
		t.Fatal("explicit matching nondefault import should be no-op")
	}
	omitted := matching
	omitted.Active, omitted.Restart, omitted.ShowInSelfService, omitted.SelfServiceRecommended = types.BoolNull(), types.BoolNull(), types.BoolNull(), types.BoolNull()
	omitted.ExecutionFrequency, omitted.RemediationScript, omitted.SelfServiceCategoryID = types.StringNull(), types.StringNull(), types.StringNull()
	proposed := remote
	proposed.SelfServiceCategoryID = types.StringNull()
	result := plan(omitted, proposed)
	want := stateFor(t, syntheticModel())
	if !result.Equal(want.Raw) {
		t.Fatal("omitted static defaults should be visible in the plan")
	}
	if reads != 1 {
		t.Fatal("planning should not make additional API requests")
	}
}

func TestRefresh404RemovesStateOtherErrorsPreserveIt(t *testing.T) {
	ctx := context.Background()
	state := stateFor(t, syntheticModel())
	for _, status := range []int{404, 401, 403, 400, 500} {
		r := mockedResource(t, status, "synthetic-token echoed secret", nil)
		resp := resource.ReadResponse{State: state}
		r.Read(ctx, resource.ReadRequest{State: state}, &resp)
		if status == 404 {
			if resp.Diagnostics.HasError() || !resp.State.Raw.IsNull() {
				t.Error("404 refresh must remove state")
			}
		} else {
			if !resp.Diagnostics.HasError() || !resp.State.Raw.Equal(state.Raw) {
				t.Error("failed refresh must preserve state")
			}
			if strings.Contains(fmt.Sprint(resp.Diagnostics), "synthetic-token") {
				t.Error("diagnostic leaked credential")
			}
		}
	}
}

func TestWriteErrorsPreservePriorState(t *testing.T) {
	ctx := context.Background()
	prior := stateFor(t, syntheticModel())
	r := mockedResource(t, 403, "synthetic-token", nil)
	update := resource.UpdateResponse{State: prior}
	r.Update(ctx, resource.UpdateRequest{Plan: tfsdk.Plan{Schema: prior.Schema, Raw: prior.Raw}, State: prior}, &update)
	if !update.Diagnostics.HasError() || !update.State.Raw.Equal(prior.Raw) {
		t.Error("update failure must preserve state")
	}
	del := resource.DeleteResponse{State: prior}
	r.Delete(ctx, resource.DeleteRequest{State: prior}, &del)
	if !del.Diagnostics.HasError() || !del.State.Raw.Equal(prior.Raw) {
		t.Error("delete failure must preserve state")
	}
	r = mockedResource(t, 404, "", nil)
	del = resource.DeleteResponse{State: prior}
	r.Delete(ctx, resource.DeleteRequest{State: prior}, &del)
	if del.Diagnostics.HasError() {
		t.Error("absent delete should succeed")
	}
}

func TestUpdate404PreservesManagedState(t *testing.T) {
	ctx := context.Background()
	prior := stateFor(t, syntheticModel())
	calls := 0
	r := mockedResource(t, 404, "not found", func(req *http.Request) {
		calls++
		if req.Method != "PATCH" {
			t.Error("unexpected mutation method")
		}
	})
	resp := resource.UpdateResponse{State: prior}
	r.Update(ctx, resource.UpdateRequest{Plan: tfsdk.Plan{Schema: prior.Schema, Raw: prior.Raw}, State: prior}, &resp)
	if !resp.Diagnostics.HasError() || !resp.State.Raw.Equal(prior.Raw) || calls != 1 {
		t.Fatal("Update 404 must surface an error and preserve state without replay")
	}
}

func TestMalformedCreateResponseRetainsKnownID(t *testing.T) {
	ctx := context.Background()
	model := syntheticModel()
	model.ID = types.StringUnknown()
	state := stateFor(t, model)
	r := mockedResource(t, 201, `{"id":"`+syntheticID+`"}`, nil)
	resp := resource.CreateResponse{State: state}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: state.Schema, Raw: state.Raw}}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("malformed create response accepted")
	}
	var saved scriptModel
	if diags := resp.State.Get(ctx, &saved); diags.HasError() {
		t.Fatal(diags)
	}
	if saved.ID.ValueString() != syntheticID {
		t.Error("known created ID was lost")
	}
}

func TestConfigurationValidationAndUnknownDeferral(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		mutate func(*scriptModel)
		bad    bool
	}{
		{"valid", func(*scriptModel) {}, false},
		{"name", func(v *scriptModel) { v.Name = types.StringValue(" ") }, true},
		{"script", func(v *scriptModel) { v.Script = types.StringValue("") }, true},
		{"frequency", func(v *scriptModel) { v.ExecutionFrequency = types.StringValue("hourly") }, true},
		{"no enforcement", func(v *scriptModel) { v.ExecutionFrequency = types.StringValue("no_enforcement") }, true},
		{"missing category", func(v *scriptModel) { v.ShowInSelfService = types.BoolValue(true) }, true},
		{"category", func(v *scriptModel) {
			v.ShowInSelfService = types.BoolValue(true)
			v.SelfServiceCategoryID = types.StringValue("bad")
		}, true},
		{"hidden category", func(v *scriptModel) { v.SelfServiceCategoryID = types.StringValue(syntheticCategory) }, true},
		{"hidden recommendation", func(v *scriptModel) { v.SelfServiceRecommended = types.BoolValue(true) }, true},
		{"unknown dependency", func(v *scriptModel) {
			v.ShowInSelfService = types.BoolUnknown()
			v.ExecutionFrequency = types.StringValue("no_enforcement")
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model := syntheticModel()
			tc.mutate(&model)
			state := stateFor(t, model)
			resp := resource.ValidateConfigResponse{}
			(&CustomScriptResource{}).ValidateConfig(ctx, resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: state.Schema, Raw: state.Raw}}, &resp)
			if resp.Diagnostics.HasError() != tc.bad {
				t.Errorf("unexpected diagnostics: %v", resp.Diagnostics)
			}
		})
	}
	unknown := syntheticModel()
	unknown.Active = types.BoolUnknown()
	state := stateFor(t, unknown)
	r := mockedResource(t, 201, "", func(*http.Request) { t.Fatal("unknown plan reached transport") })
	resp := resource.CreateResponse{State: state}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: state.Schema, Raw: state.Raw}}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Error("unknown plan accepted")
	}
}

func TestConfigureRejectsWrongClientType(t *testing.T) {
	r := &CustomScriptResource{}
	resp := resource.ConfigureResponse{}
	r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: "wrong"}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Error("wrong client type accepted")
	}
}

type protocolTestProvider struct {
	IruProvider
	c *client.Client
}

func (p *protocolTestProvider) Configure(_ context.Context, _ fwprovider.ConfigureRequest, resp *fwprovider.ConfigureResponse) {
	resp.ResourceData = p.c
}

func TestMatchingConfigurationHasNoOpProtocolPlan(t *testing.T) {
	ctx := context.Background()
	state := stateFor(t, syntheticModel())
	configModel := syntheticModel()
	configModel.ID = types.StringNull()
	configState := stateFor(t, configModel)
	r := mockedResource(t, 200, modelJSON(t, syntheticModel()), nil)
	server := providerserver.NewProtocol6(&protocolTestProvider{IruProvider: IruProvider{version: "test"}, c: r.client})()
	_, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	tfType := state.Schema.Type().TerraformType(ctx)
	prior, err := tfprotov6.NewDynamicValue(tfType, state.Raw)
	if err != nil {
		t.Fatal(err)
	}
	config, err := tfprotov6.NewDynamicValue(tfType, configState.Raw)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := server.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{TypeName: "iru_custom_script", PriorState: &prior, ProposedNewState: &prior, Config: &config})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range plan.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			t.Fatalf("plan error: %s", d.Detail)
		}
	}
	if plan.PlannedState == nil {
		t.Fatal("missing plan")
	}
	planned, err := plan.PlannedState.Unmarshal(tfType)
	if err != nil {
		t.Fatal(err)
	}
	if !planned.Equal(state.Raw) {
		t.Fatal("matching configuration changed the protocol plan")
	}
}
