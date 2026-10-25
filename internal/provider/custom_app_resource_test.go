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

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
)

func appSchema(t *testing.T) schema.Schema {
	t.Helper()
	var resp resource.SchemaResponse
	(&CustomAppResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if diags := resp.Schema.ValidateImplementation(context.Background()); diags.HasError() {
		t.Fatal(diags)
	}
	return resp.Schema
}

func appStateFor(t *testing.T, data appModel) tfsdk.State {
	t.Helper()
	s := appSchema(t)
	state := tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(context.Background()), nil)}
	if diags := state.Set(context.Background(), &data); diags.HasError() {
		t.Fatal(diags)
	}
	return state
}

func syntheticApp() appModel {
	return appState(client.App{ID: syntheticID, AppInput: client.AppInput{Name: "Synthetic app", FileKey: "companies/synthetic/library/custom_apps/example.pkg", InstallType: "package", InstallEnforcement: "install_once"}}, appModel{})
}

func appModelJSON(t *testing.T, data appModel) string {
	t.Helper()
	encoded, err := json.Marshal(client.App{ID: data.ID.ValueString(), AppInput: data.input()})
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func mockedAppResource(t *testing.T, status int, body string, check func(*http.Request)) *CustomAppResource {
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
	return &CustomAppResource{client: c}
}

func TestAppResourceLifecycleAndImport(t *testing.T) {
	ctx := context.Background()
	model := syntheticApp()
	state := appStateFor(t, model)
	r := mockedAppResource(t, 201, appModelJSON(t, model), nil)
	created := resource.CreateResponse{State: state}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: state.Schema, Raw: state.Raw}}, &created)
	if created.Diagnostics.HasError() || !created.State.Raw.Equal(state.Raw) {
		t.Fatal("create mismatch", created.Diagnostics)
	}
	imported := resource.ImportStateResponse{State: tfsdk.State{Schema: state.Schema, Raw: tftypes.NewValue(state.Schema.Type().TerraformType(ctx), nil)}}
	r.ImportState(ctx, resource.ImportStateRequest{ID: strings.ToUpper(syntheticID)}, &imported)
	r = mockedAppResource(t, 200, appModelJSON(t, model), nil)
	read := resource.ReadResponse{State: imported.State}
	r.Read(ctx, resource.ReadRequest{State: imported.State}, &read)
	if imported.Diagnostics.HasError() || read.Diagnostics.HasError() || !read.State.Raw.Equal(state.Raw) {
		t.Fatal("import/read must reconstruct all managed fields", read.Diagnostics)
	}
	second := resource.ReadResponse{State: read.State}
	r.Read(ctx, resource.ReadRequest{State: read.State}, &second)
	if second.Diagnostics.HasError() || !second.State.Raw.Equal(read.State.Raw) {
		t.Fatal("repeat read unstable")
	}
	model.Name = types.StringValue("Synthetic updated")
	plan := appStateFor(t, model)
	r = mockedAppResource(t, 200, appModelJSON(t, model), func(req *http.Request) {
		if req.Method != "PATCH" || req.ParseForm() != nil {
			t.Fatal("invalid metadata update")
		}
		if _, ok := req.Form["file_key"]; ok {
			t.Fatal("metadata update unnecessarily replaced installer")
		}
	})
	updated := resource.UpdateResponse{State: read.State}
	r.Update(ctx, resource.UpdateRequest{Plan: tfsdk.Plan{Schema: plan.Schema, Raw: plan.Raw}, State: read.State}, &updated)
	if updated.Diagnostics.HasError() || !updated.State.Raw.Equal(plan.Raw) {
		t.Fatal("update mismatch", updated.Diagnostics)
	}
	r = mockedAppResource(t, 204, "", nil)
	deleted := resource.DeleteResponse{State: updated.State}
	r.Delete(ctx, resource.DeleteRequest{State: updated.State}, &deleted)
	if deleted.Diagnostics.HasError() {
		t.Fatal(deleted.Diagnostics)
	}
}

func TestAppErrorsPreserveStateAndPartialCreateID(t *testing.T) {
	ctx := context.Background()
	state := appStateFor(t, syntheticApp())
	for _, status := range []int{401, 403, 404, 503} {
		calls := 0
		r := mockedAppResource(t, status, "synthetic-token echoed signed-url", func(*http.Request) { calls++ })
		read := resource.ReadResponse{State: state}
		r.Read(ctx, resource.ReadRequest{State: state}, &read)
		if status == 404 {
			if read.Diagnostics.HasError() || !read.State.Raw.IsNull() {
				t.Fatal("read 404 must remove only the app state")
			}
		} else if !read.Diagnostics.HasError() || !read.State.Raw.Equal(state.Raw) {
			t.Fatal("read error must preserve app state")
		}
		// Use a nonretryable status here; read retry behavior is tested centrally.
		if status == 503 {
			continue
		}
		calls = 0
		updated := resource.UpdateResponse{State: state}
		r.Update(ctx, resource.UpdateRequest{Plan: tfsdk.Plan{Schema: state.Schema, Raw: state.Raw}, State: state}, &updated)
		if !updated.Diagnostics.HasError() || !updated.State.Raw.Equal(state.Raw) || calls != 1 || strings.Contains(fmt.Sprint(updated.Diagnostics), "synthetic-token") {
			t.Fatal("write error must preserve state, redact, and never replay")
		}
	}
	r := mockedAppResource(t, 201, `{"id":"`+syntheticID+`","name":"Synthetic partial"}`, nil)
	partialPlan := syntheticApp()
	partialPlan.ID, partialPlan.SHA256, partialPlan.FileSize = types.StringUnknown(), types.StringUnknown(), types.Int64Unknown()
	plannedState := appStateFor(t, partialPlan)
	created := resource.CreateResponse{State: state}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: plannedState.Schema, Raw: plannedState.Raw}}, &created)
	var result appModel
	if diags := created.State.Get(ctx, &result); diags.HasError() || !created.Diagnostics.HasError() || result.ID.ValueString() != syntheticID || !result.SHA256.IsNull() || !result.FileSize.IsNull() {
		t.Fatal("partial create must retain returned ID")
	}
}

func TestAppUnknownInputsAndSensitiveSchema(t *testing.T) {
	ctx := context.Background()
	model := syntheticApp()
	model.FileKey = types.StringUnknown()
	state := appStateFor(t, model)
	r := mockedAppResource(t, 201, "", func(*http.Request) { t.Fatal("unknown input reached transport") })
	validation := resource.ValidateConfigResponse{}
	r.ValidateConfig(ctx, resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: state.Schema, Raw: state.Raw}}, &validation)
	if validation.Diagnostics.HasError() {
		t.Fatal("unknown dependent configuration must defer validation")
	}
	created := resource.CreateResponse{State: state}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: state.Schema, Raw: state.Raw}}, &created)
	if !created.Diagnostics.HasError() {
		t.Fatal("unresolved write plan accepted")
	}
	s := appSchema(t)
	for _, name := range []string{"audit_script", "preinstall_script", "postinstall_script"} {
		if !s.Attributes[name].IsSensitive() {
			t.Fatal("script content is not marked sensitive")
		}
	}
	if s.Attributes["file_url"] != nil {
		t.Fatal("signed URLs must not be in resource state")
	}
	bad := resource.ImportStateResponse{State: state}
	r.ImportState(ctx, resource.ImportStateRequest{ID: "../synthetic"}, &bad)
	if !bad.Diagnostics.HasError() {
		t.Fatal("invalid import ID accepted")
	}
}
