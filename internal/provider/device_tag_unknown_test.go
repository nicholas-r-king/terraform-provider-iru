// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestUnknownDeviceTagMemberDefersValidationButCannotApply(t *testing.T) {
	ctx := context.Background()
	for _, ade := range []bool{false, true} {
		base := emptyDeviceSettingsObject(ade)
		attrs := base.Attributes()
		attrs["device_id"] = types.StringValue(syntheticID)
		attrs["tags"] = types.SetValueMust(types.StringType, []attr.Value{types.StringUnknown()})
		value := types.ObjectValueMust(base.AttributeTypes(ctx), attrs)
		if value.IsUnknown() || attrs["tags"].IsUnknown() || deviceConfigDiagnostics(ctx, value, ade).HasError() || settingsResolved(value) {
			t.Fatal("known set with unknown tag member was prematurely validated or treated as resolved")
		}
		var diagnostics diag.Diagnostics
		r := &DeviceSettingsResource{ade: ade}
		if _, ok := r.apply(ctx, value, value, &diagnostics, nil); ok || !diagnostics.HasError() {
			t.Fatal("unresolved tag member reached the apply path")
		}
	}
}
