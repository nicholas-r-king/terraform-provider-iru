// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Iru can strip outer script whitespace. Preserve a known configured spelling
// when only that normalization differs; keep substantive remote changes as drift.
func scriptValue(remote string, prior types.String) types.String {
	if !prior.IsNull() && !prior.IsUnknown() && strings.TrimSpace(remote) == strings.TrimSpace(prior.ValueString()) {
		return prior
	}
	return types.StringValue(remote)
}
