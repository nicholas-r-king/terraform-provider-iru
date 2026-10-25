// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
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

const syntheticProfileXML = `<?xml version="1.0"?><plist version="1.0"><dict><key>PayloadDisplayName</key><string>Synthetic profile</string></dict></plist>`

func syntheticProfile() profileModel {
	return profileState(client.Profile{ID: syntheticID, MDMIdentifier: "com.example.synthetic", ProfileInput: client.ProfileInput{Name: "Synthetic profile", Profile: syntheticProfileXML, RunsOnMac: true}})
}

func profileStateFor(t *testing.T, data profileModel) tfsdk.State {
	t.Helper()
	var resp resource.SchemaResponse
	(&CustomProfileResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if diags := resp.Schema.ValidateImplementation(context.Background()); diags.HasError() {
		t.Fatal(diags)
	}
	state := tfsdk.State{Schema: resp.Schema, Raw: tftypes.NewValue(resp.Schema.Type().TerraformType(context.Background()), nil)}
	if diags := state.Set(context.Background(), &data); diags.HasError() {
		t.Fatal(diags)
	}
	return state
}

func profileModelJSON(t *testing.T, data profileModel) string {
	t.Helper()
	encoded, err := json.Marshal(client.Profile{ID: data.ID.ValueString(), MDMIdentifier: data.MDMIdentifier.ValueString(), ProfileInput: data.input()})
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func mockedProfileResource(t *testing.T, status int, body string, check func(*http.Request)) *CustomProfileResource {
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
	return &CustomProfileResource{client: c}
}

func TestProfileResourceImportAndMetadataUpdate(t *testing.T) {
	ctx := context.Background()
	model := syntheticProfile()
	state := profileStateFor(t, model)
	r := mockedProfileResource(t, 200, profileModelJSON(t, model), nil)
	imported := resource.ImportStateResponse{State: tfsdk.State{Schema: state.Schema, Raw: tftypes.NewValue(state.Schema.Type().TerraformType(ctx), nil)}}
	r.ImportState(ctx, resource.ImportStateRequest{ID: strings.ToUpper(syntheticID)}, &imported)
	read := resource.ReadResponse{State: imported.State}
	r.Read(ctx, resource.ReadRequest{State: imported.State}, &read)
	if imported.Diagnostics.HasError() || read.Diagnostics.HasError() || !read.State.Raw.Equal(state.Raw) {
		t.Fatal("profile import did not reconstruct state")
	}
	model.Name = types.StringValue("Synthetic renamed")
	plan := profileStateFor(t, model)
	r = mockedProfileResource(t, 200, profileModelJSON(t, model), func(req *http.Request) {
		if req.Method != "PATCH" || req.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Error("metadata update must not upload a profile again")
		}
	})
	updated := resource.UpdateResponse{State: read.State}
	r.Update(ctx, resource.UpdateRequest{Plan: tfsdk.Plan{Schema: plan.Schema, Raw: plan.Raw}, State: read.State}, &updated)
	if updated.Diagnostics.HasError() || !updated.State.Raw.Equal(plan.Raw) {
		t.Fatal("profile metadata update mismatch", updated.Diagnostics)
	}
	if !state.Schema.(schema.Schema).Attributes["profile"].IsSensitive() {
		t.Fatal("profile content must be sensitive")
	}
}

func TestProfilePartialCreateAndReadErrorState(t *testing.T) {
	ctx := context.Background()
	model := syntheticProfile()
	state := profileStateFor(t, model)
	for _, status := range []int{404, 401, 403} {
		r := mockedProfileResource(t, status, "synthetic-profile-secret", nil)
		read := resource.ReadResponse{State: state}
		r.Read(ctx, resource.ReadRequest{State: state}, &read)
		if status == 404 {
			if read.Diagnostics.HasError() || !read.State.Raw.IsNull() {
				t.Fatal("profile 404 must remove state")
			}
		} else if !read.Diagnostics.HasError() || !read.State.Raw.Equal(state.Raw) {
			t.Fatal("failed profile read must preserve state")
		}
	}
	model.ID, model.MDMIdentifier = types.StringUnknown(), types.StringUnknown()
	plan := profileStateFor(t, model)
	r := mockedProfileResource(t, 201, `{"id":"`+syntheticID+`"}`, nil)
	created := resource.CreateResponse{State: state}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: plan.Schema, Raw: plan.Raw}}, &created)
	var result profileModel
	if diags := created.State.Get(ctx, &result); diags.HasError() || !created.Diagnostics.HasError() || result.ID.ValueString() != syntheticID || !result.MDMIdentifier.IsNull() {
		t.Fatal("profile partial ID recovery must contain no unknown computed state")
	}
	model.Profile = types.StringUnknown()
	unknown := profileStateFor(t, model)
	r = mockedProfileResource(t, 201, "", func(*http.Request) { t.Fatal("unknown XML reached transport") })
	resp := resource.CreateResponse{State: state}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: unknown.Schema, Raw: unknown.Raw}}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("unknown profile plan accepted")
	}
}

func TestProfileUpdate404PreservesStateWithoutReplay(t *testing.T) {
	ctx := context.Background()
	state := profileStateFor(t, syntheticProfile())
	calls := 0
	r := mockedProfileResource(t, 404, "synthetic-token profile-content", func(req *http.Request) {
		calls++
		if req.Method != "PATCH" || req.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Error("metadata-only profile update must use a concrete form body")
		}
	})
	resp := resource.UpdateResponse{State: state}
	r.Update(ctx, resource.UpdateRequest{Plan: tfsdk.Plan{Schema: state.Schema, Raw: state.Raw}, State: state}, &resp)
	if !resp.Diagnostics.HasError() || calls != 1 || !resp.State.Raw.Equal(state.Raw) {
		t.Fatal("Update 404 must diagnose, preserve prior state and avoid replay")
	}
}
