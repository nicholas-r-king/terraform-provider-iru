// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Preserve only the observed removal of final line endings, in one direction.
// Spaces, tabs and leading whitespace can change executable semantics and must
// remain visible as drift. Never rewrite the configured source bytes.
func scriptValue(remote string, prior types.String) types.String {
	if !prior.IsNull() && !prior.IsUnknown() && (remote == prior.ValueString() || remote == strings.TrimRight(prior.ValueString(), "\r\n")) {
		return prior
	}
	return types.StringValue(remote)
}
