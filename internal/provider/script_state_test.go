// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
)

func TestScriptStateRetainsEquivalentWhitespaceOnly(t *testing.T) {
	prior := types.StringValue("#!/bin/sh\ncat <<'EOF'\nsynthetic\nEOF\n")
	remote := strings.TrimSpace(prior.ValueString())
	if !scriptValue(remote, prior).Equal(prior) {
		t.Fatal("API whitespace normalization changed a known configured value")
	}
	changed := "#!/bin/sh\necho substantive-change"
	if scriptValue(changed, prior).ValueString() != changed {
		t.Fatal("whitespace equivalence hid substantive script drift")
	}
	for _, empty := range []types.String{types.StringNull(), types.StringUnknown()} {
		if scriptValue(remote, empty).ValueString() != remote {
			t.Fatal("import/read invented unknown original whitespace")
		}
	}
}

func TestAppApplyRetainsScriptsAfterAPINormalization(t *testing.T) {
	ctx := context.Background()
	model := syntheticApp()
	model.InstallEnforcement = types.StringValue("continuously_enforce")
	model.AuditScript = types.StringValue("#!/bin/sh\nexit 0\n")
	model.PreinstallScript = types.StringValue("#!/bin/sh\ncat <<'EOF'\nsynthetic\nEOF\n")
	model.PostinstallScript = types.StringValue("#!/bin/sh\nexit 0\n")
	remote := client.App{ID: syntheticID, AppInput: model.input()}
	remote.AuditScript, remote.PreinstallScript, remote.PostinstallScript = strings.TrimSpace(remote.AuditScript), strings.TrimSpace(remote.PreinstallScript), strings.TrimSpace(remote.PostinstallScript)
	result := appState(remote, model)
	state, want := appStateFor(t, result), appStateFor(t, model)
	if !state.Raw.Equal(want.Raw) {
		t.Fatal("normalized script response would violate planned known values")
	}
	// Exercise Create's state mapping with the normalized response as well.
	encoded, err := json.Marshal(remote)
	if err != nil {
		t.Fatal(err)
	}
	r := mockedAppResource(t, 201, string(encoded), nil)
	response := resource.CreateResponse{State: want}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: want.Schema, Raw: want.Raw}}, &response)
	if response.Diagnostics.HasError() || !response.State.Raw.Equal(want.Raw) {
		t.Fatal("create lost planned script spelling", response.Diagnostics)
	}
}
