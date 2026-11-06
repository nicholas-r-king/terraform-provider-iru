// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
)

func TestTerraformCLIBlueprintClassicCreationRejected(t *testing.T) {
	var calls atomic.Int32
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	write("resource \"iru_blueprint\" \"example\" {\n name=\"Synthetic classic\"\n type=\"classic\"\n}\n")
	for _, args := range [][]string{{"plan", "-no-color"}, {"apply", "-auto-approve", "-no-color"}} {
		if out := run(1, args...); !strings.Contains(out, "Classic Blueprint creation unsupported") {
			t.Fatal("new classic creation did not explain the platform compatibility limit")
		}
	}
	write("resource \"iru_blueprint\" \"example\" {\n name=\"Synthetic default map\"\n color=\"orange-500\"\n}\n")
	if out := run(1, "plan", "-no-color"); !strings.Contains(out, "Unsupported map appearance") {
		t.Fatal("omitted type bypassed the map appearance guard")
	}
	if calls.Load() != 0 {
		t.Fatal("invalid new Blueprint plan reached the API")
	}
}

func TestTerraformCLIBlueprintLegacyTypePreservedAndMigrationObserved(t *testing.T) {
	var mu sync.Mutex
	object := client.ManagedBlueprint{ID: syntheticID, Name: "Synthetic legacy", Type: "classic", Color: "aqua-800", Icon: "ss-files"}
	code, active := "000123", false
	object.EnrollmentCode.Code, object.EnrollmentCode.Active = &code, &active
	patches := 0
	run, write := mockTerraform(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if req.URL.Path != "/api/v1/blueprints/"+syntheticID {
			t.Error("legacy test attempted creation or a different identity")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		switch req.Method {
		case http.MethodGet:
		case http.MethodPatch:
			if req.ParseForm() != nil || req.Form.Has("type") || req.Form.Get("enrollment_code.is_active") != "false" {
				t.Error("legacy metadata change attempted conversion or enrollment activation")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			patches++
			object.Name = req.Form.Get("name")
		default:
			t.Error("legacy compatibility test attempted create/delete")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(object)
	}))
	config := func(name, extra string) {
		write(fmt.Sprintf("resource \"iru_blueprint\" \"example\" {\n name=%q\n%s\n}\n", name, extra))
	}
	config("Synthetic legacy", "")
	run(0, "import", "-no-color", "iru_blueprint.example", syntheticID)
	run(0, "plan", "-no-color", "-detailed-exitcode")
	config("Synthetic renamed", "")
	run(0, "apply", "-auto-approve", "-no-color")
	run(0, "plan", "-no-color", "-detailed-exitcode")
	mu.Lock()
	object.Type = "map" // Model vendor conversion observed through refresh.
	mu.Unlock()
	run(0, "plan", "-no-color", "-detailed-exitcode")
	config("Synthetic renamed", "type=\"classic\"")
	if out := run(1, "plan", "-no-color"); !strings.Contains(out, "Blueprint replacement blocked") {
		t.Fatal("explicit stale type attempted destructive conversion")
	}
	mu.Lock()
	defer mu.Unlock()
	if patches != 1 {
		t.Fatal("import/planning performed extra writes")
	}
}

func TestBlueprintOldSavedClassicCreatePlanRefused(t *testing.T) {
	ctx := context.Background()
	c, err := client.New("https://synthetic.api.iru.com", "synthetic-token", &http.Client{Transport: syntheticTransport(func(*http.Request) (*http.Response, error) {
		t.Error("saved classic creation reached the API")
		return nil, errors.New("unexpected transport")
	})})
	if err != nil {
		t.Fatal(err)
	}
	r := &BlueprintResource{client: c}
	var schema resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schema)
	state := tfsdk.State{Schema: schema.Schema, Raw: tftypes.NewValue(schema.Schema.Type().TerraformType(ctx), nil)}
	model := blueprintModel{ID: types.StringUnknown(), Name: types.StringValue("Synthetic saved plan"), Type: types.StringValue("classic"), Description: types.StringValue(""), Color: types.StringUnknown(), Icon: types.StringUnknown(), EnrollmentCode: types.StringUnknown(), EnrollmentActive: types.BoolValue(false), SourceType: types.StringNull(), SourceID: types.StringNull()}
	if diags := state.Set(ctx, &model); diags.HasError() {
		t.Fatal(diags)
	}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: schema.Schema, Raw: tftypes.NewValue(schema.Schema.Type().TerraformType(ctx), nil)}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: schema.Schema, Raw: state.Raw}}, &resp)
	if !resp.Diagnostics.HasError() || !strings.Contains(fmt.Sprint(resp.Diagnostics), "classic Blueprint creation is unsupported") || !resp.State.Raw.IsNull() {
		t.Fatal("old saved classic create plan was not safely refused")
	}
}
