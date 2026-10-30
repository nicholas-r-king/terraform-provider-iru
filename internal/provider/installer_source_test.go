// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestInstallerSourcePlanPreservesOnlyUnchangedContent(t *testing.T) {
	ctx := context.Background()
	sum := strings.Repeat("a", 64)
	for _, tc := range []struct {
		name                                   string
		configuredKey, source, hash, priorHash types.String
		unknown                                bool
	}{
		{"metadata update", types.StringNull(), types.StringValue("synthetic.pkg"), types.StringValue(sum), types.StringValue(sum), false},
		{"checksum case", types.StringNull(), types.StringValue("synthetic.pkg"), types.StringValue(strings.ToUpper(sum)), types.StringValue(sum), false},
		{"new bytes", types.StringNull(), types.StringValue("synthetic.pkg"), types.StringValue(strings.Repeat("b", 64)), types.StringValue(sum), true},
		{"imported key adoption", types.StringNull(), types.StringNull(), types.StringNull(), types.StringNull(), false},
		{"unresolved external key", types.StringUnknown(), types.StringNull(), types.StringNull(), types.StringNull(), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prior := syntheticApp()
			prior.SourceFile, prior.SourceSHA256 = tc.source, tc.priorHash
			state := appStateFor(t, prior)
			config := prior
			config.FileKey, config.SourceFile, config.SourceSHA256 = tc.configuredKey, tc.source, tc.hash
			cfg := appStateFor(t, config)
			planned := config
			planned.FileKey = types.StringUnknown()
			planned.Name = types.StringValue("Synthetic metadata update")
			ps := appStateFor(t, planned)
			req := resource.ModifyPlanRequest{State: state, Config: tfsdk.Config{Schema: cfg.Schema, Raw: cfg.Raw}, Plan: tfsdk.Plan{Schema: ps.Schema, Raw: ps.Raw}}
			resp := resource.ModifyPlanResponse{Plan: req.Plan}
			(&CustomAppResource{}).ModifyPlan(ctx, req, &resp)
			var key types.String
			resp.Diagnostics.Append(resp.Plan.GetAttribute(ctx, path.Root("file_key"), &key)...)
			if resp.Diagnostics.HasError() || key.IsUnknown() != tc.unknown || (!tc.unknown && key.ValueString() != prior.FileKey.ValueString()) {
				t.Fatal("installer plan lost content-change semantics", resp.Diagnostics)
			}
		})
	}
}

func TestInstallerSourceValidationAndRemoteDrift(t *testing.T) {
	null := types.StringNull()
	sum := strings.Repeat("a", 64)
	filename := types.StringValue("synthetic.pkg")
	hash := types.StringValue(sum)
	for _, input := range [][3]types.String{{types.StringValue("existing"), filename, hash}, {null, filename, null}, {null, null, hash}, {null, filename, types.StringValue("bad")}, {null, types.StringValue(" "), hash}} {
		if validateInstallerSource(input[0], input[1], input[2]) == nil {
			t.Fatal("invalid installer source accepted")
		}
	}
	if validateInstallerSource(null, null, null) != nil || validateInstallerSource(null, filename, hash) != nil {
		t.Fatal("import adoption or local source rejected")
	}
	key := types.StringValue("synthetic-original.pkg")
	different := strings.Repeat("b", 64)
	if got := sourceChecksumAfterRead(filename, hash, key, "synthetic-changed.pkg", nil); !got.IsNull() {
		t.Fatal("unknown bytes behind a changed key were trusted")
	}
	if got := sourceChecksumAfterRead(filename, hash, key, "synthetic-original.pkg", &different); got.ValueString() != different {
		t.Fatal("reported installer drift was hidden")
	}
	upper := types.StringValue(strings.ToUpper(sum))
	if got := sourceChecksumAfterRead(filename, upper, key, "synthetic-original.pkg", &sum); !got.Equal(upper) {
		t.Fatal("equivalent configured hash spelling changed")
	}
}
