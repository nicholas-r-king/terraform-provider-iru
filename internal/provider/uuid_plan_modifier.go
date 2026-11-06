// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
)

// UUID text is case-insensitive, but preserve the configured spelling in the
// plan/state rather than changing a known configuration value during planning.
func uuidReplacementModifier() planmodifier.String {
	return stringplanmodifier.RequiresReplaceIf(func(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
		resp.RequiresReplace = req.PlanValue.IsUnknown() || req.StateValue.IsUnknown() || !strings.EqualFold(req.PlanValue.ValueString(), req.StateValue.ValueString())
	}, "A different UUID requires replacement; casing-only changes retain the same identity.", "A different UUID requires replacement; casing-only changes retain the same identity.")
}
