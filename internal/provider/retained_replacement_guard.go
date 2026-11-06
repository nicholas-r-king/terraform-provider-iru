// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource"
)

const retainedExistingPlanKey = "iru_retain_existing_v1"

const retainedReplacementFailureDetail = "If this is a replacement failure, a new object may already exist before this DELETE refusal; preserve active/deposed state and reconcile both before retrying. Configure lifecycle prevent_destroy=true and replan to reject tainted replacements in Terraform Core."

// Core replans a forced replacement with null prior state, carrying the first
// plan's private data. Mark that first pass so it cannot masquerade as an ordinary
// new create on the second pass. No token, locator or other value is stored.
// Tainted objects are a distinct Core limitation: their prior state AND private
// data are omitted, so this cannot distinguish them from an ordinary new create.
func blockRetainedReplacement(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse, kind, detail string) bool {
	if req.Plan.Raw.IsNull() {
		return false // Each resource's unconditional deletion guard handles this.
	}
	if !req.State.Raw.IsNull() {
		if resp.Private != nil {
			resp.Diagnostics.Append(resp.Private.SetKey(ctx, retainedExistingPlanKey, []byte("true"))...)
		}
		return resp.Diagnostics.HasError()
	}
	if req.Private == nil {
		return false
	}
	marker, diags := req.Private.GetKey(ctx, retainedExistingPlanKey)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return true
	}
	if len(marker) != 0 {
		resp.Diagnostics.AddError(kind+" replacement blocked", detail+" Terraform is replanning an existing object as a new create. No replacement create was attempted. Remove the replacement request and retain/import the existing object.")
		return true
	}
	return false
}
