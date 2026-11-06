// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"testing"
)

// Delete must refuse independently of ModifyPlan: Terraform can load a saved
// plan produced by the earlier release, and a caller can bypass planning.
func TestDestructiveDeleteRefusesWithoutReadingStateOrCallingClient(t *testing.T) {
	for _, r := range []resource.Resource{NewBlueprintResource(), NewADEIntegrationResource()} {
		var response resource.DeleteResponse
		r.Delete(context.Background(), resource.DeleteRequest{}, &response)
		if !response.Diagnostics.HasError() {
			t.Fatalf("%T accepted destruction without safety evidence", r)
		}
		if response.Diagnostics[0].Summary() != "Blueprint deletion blocked" && response.Diagnostics[0].Summary() != "ADE integration deletion blocked" {
			t.Fatalf("%T did not report destructive-delete guard", r)
		}
	}
}
