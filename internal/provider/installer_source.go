// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"errors"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
)

func validateInstallerSource(fileKey, filename, checksum types.String) error {
	if !filename.IsNull() && !filename.IsUnknown() {
		if strings.TrimSpace(filename.ValueString()) == "" {
			return errors.New("source_file must not be blank")
		}
		if !fileKey.IsNull() && !fileKey.IsUnknown() {
			return errors.New("configure file_key or source_file, not both")
		}
		if checksum.IsNull() {
			return errors.New("source_file requires source_sha256; use filesha256(source_file)")
		}
	} else if filename.IsNull() && !checksum.IsNull() {
		return errors.New("source_sha256 requires source_file")
	}
	if !checksum.IsNull() && !checksum.IsUnknown() && !client.ValidInstallerSHA256(checksum.ValueString()) {
		return errors.New("source_sha256 must be a SHA256 digest")
	}
	return nil
}

func sameSourceChecksum(left, right types.String) bool {
	if left.IsUnknown() || right.IsUnknown() {
		return false
	}
	if left.IsNull() || right.IsNull() {
		return left.IsNull() && right.IsNull()
	}
	return strings.EqualFold(left.ValueString(), right.ValueString())
}

func uploadedInstallerMatches(filename, checksum types.String, key, actualKey string, reported *string) bool {
	return filename.IsNull() || (key == actualKey && (reported == nil || strings.EqualFold(*reported, checksum.ValueString())))
}

// Optional/computed file_key may be adopted during import. Preserve it on
// metadata-only updates; a changed source checksum leaves the new key unknown.
func preserveInstallerKey(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() || req.State.Raw.IsNull() {
		return
	}
	var key, configuredKey, filename, checksum, priorChecksum, priorKey types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("file_key"), &key)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("file_key"), &configuredKey)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("source_file"), &filename)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("source_sha256"), &checksum)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("source_sha256"), &priorChecksum)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("file_key"), &priorKey)...)
	if resp.Diagnostics.HasError() || !key.IsUnknown() || !configuredKey.IsNull() || filename.IsUnknown() {
		return
	}
	if filename.IsNull() || (!checksum.IsUnknown() && !checksum.IsNull() && !priorChecksum.IsUnknown() && !priorChecksum.IsNull() && strings.EqualFold(checksum.ValueString(), priorChecksum.ValueString())) {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("file_key"), priorKey)...)
	}
}

func sourceChecksumAfterRead(filename, priorChecksum, priorKey types.String, key string, reported *string) types.String {
	if filename.IsNull() || filename.IsUnknown() {
		return priorChecksum
	}
	if reported != nil {
		if !priorChecksum.IsUnknown() && !priorChecksum.IsNull() && strings.EqualFold(*reported, priorChecksum.ValueString()) {
			return priorChecksum
		}
		return types.StringValue(*reported)
	}
	if !priorKey.IsNull() && !priorKey.IsUnknown() && priorKey.ValueString() != key {
		// Without a reported checksum, a changed remote key cannot be assumed to
		// contain the configured bytes. Plan a conservative re-upload.
		return types.StringNull()
	}
	return priorChecksum
}
