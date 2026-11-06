// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
)

func TestUUIDReplacementModifierPreservesConfiguredValues(t *testing.T) {
	const id = "aaaaaaaa-bbbb-4ccc-8ddd-111111111111"
	for _, tc := range []struct {
		name                  string
		state, plan           types.String
		create, destroy, want bool
	}{
		{name: "same", state: types.StringValue(id), plan: types.StringValue(id)},
		{name: "case", state: types.StringValue(id), plan: types.StringValue(strings.ToUpper(id))},
		{name: "different", state: types.StringValue(id), plan: types.StringValue(syntheticID), want: true},
		{name: "unknown-plan", state: types.StringValue(id), plan: types.StringUnknown(), want: true},
		{name: "unknown-state", state: types.StringUnknown(), plan: types.StringValue(id), want: true},
		{name: "new", state: types.StringNull(), plan: types.StringValue(id), create: true},
		{name: "destroy", state: types.StringValue(id), plan: types.StringNull(), destroy: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := tftypes.NewValue(tftypes.String, "synthetic-root")
			null := tftypes.NewValue(tftypes.String, nil)
			req := planmodifier.StringRequest{StateValue: tc.state, PlanValue: tc.plan,
				State: tfsdk.State{Raw: raw}, Plan: tfsdk.Plan{Raw: raw}}
			if tc.create {
				req.State.Raw = null
			}
			if tc.destroy {
				req.Plan.Raw = null
			}
			resp := planmodifier.StringResponse{PlanValue: tc.plan}
			uuidReplacementModifier().PlanModifyString(context.Background(), req, &resp)
			if resp.Diagnostics.HasError() || resp.RequiresReplace != tc.want || !resp.PlanValue.Equal(tc.plan) {
				t.Fatal("UUID replacement decision changed a known value or handled unresolved identity unsafely")
			}
		})
	}
}

func TestNoteUpdateRejectsInPlaceIdentityChanges(t *testing.T) {
	ctx := context.Background()
	var schemaResponse resource.SchemaResponse
	(&DeviceNoteResource{}).Schema(ctx, resource.SchemaRequest{}, &schemaResponse)
	stateFor := func(value deviceNoteModel) tfsdk.State {
		state := tfsdk.State{Schema: schemaResponse.Schema, Raw: tftypes.NewValue(schemaResponse.Schema.Type().TerraformType(ctx), nil)}
		if diags := state.Set(ctx, &value); diags.HasError() {
			t.Fatal(diags)
		}
		return state
	}
	prior := deviceNoteModel{ID: types.StringValue(syntheticID), DeviceID: types.StringValue(syntheticCategory), Content: types.StringValue("Synthetic content")}
	for _, change := range []string{"different-device", "different-note", "case-only"} {
		t.Run(change, func(t *testing.T) {
			next := prior
			switch change {
			case "different-device":
				next.DeviceID = types.StringValue(syntheticID)
			case "different-note":
				next.ID = types.StringValue(syntheticCategory)
			case "case-only":
				next.DeviceID = types.StringValue(strings.ToUpper(syntheticCategory))
			}
			calls := 0
			c, err := client.New("https://synthetic.api.iru.com", "synthetic-token", &http.Client{Transport: syntheticTransport(func(*http.Request) (*http.Response, error) {
				calls++
				return nil, errors.New("unexpected synthetic transport")
			})})
			if err != nil {
				t.Fatal(err)
			}
			before, planned := stateFor(prior), stateFor(next)
			response := resource.UpdateResponse{State: before}
			(&DeviceNoteResource{client: c}).Update(ctx, resource.UpdateRequest{State: before, Plan: tfsdk.Plan{Schema: planned.Schema, Raw: planned.Raw}}, &response)
			if response.Diagnostics.HasError() != (change != "case-only") || calls != 0 {
				t.Fatal("identity-only update mutated a Note or allowed an in-place move")
			}
			var got deviceNoteModel
			if response.State.Get(ctx, &got).HasError() {
				t.Fatal("update lost its state")
			}
			want := prior
			if change == "case-only" {
				want = next
			}
			if !got.ID.Equal(want.ID) || !got.DeviceID.Equal(want.DeviceID) || !got.Content.Equal(want.Content) {
				t.Fatal("identity check lost the locator/content or rewrote configured casing")
			}
		})
	}
}
