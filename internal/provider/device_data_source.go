// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
)

type DeviceDataSource struct {
	client *client.Client
	ade    bool
}

func NewDeviceDataSource() datasource.DataSource    { return &DeviceDataSource{} }
func NewADEDeviceDataSource() datasource.DataSource { return &DeviceDataSource{ade: true} }
func (r *DeviceDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	name := "_device"
	if r.ade {
		name = "_ade_device"
	}
	resp.TypeName = req.ProviderTypeName + name
}
func (r *DeviceDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attrs := map[string]schema.Attribute{
		"device_id":    schema.StringAttribute{Required: true, MarkdownDescription: "Exact device record UUID. For ADE this is the ADE-device UUID, not its enrolled MDM record."},
		"id":           schema.StringAttribute{Computed: true, MarkdownDescription: "Canonical device UUID."},
		"blueprint_id": schema.StringAttribute{Computed: true, MarkdownDescription: "Assigned Blueprint UUID. ADE routing is represented by an empty string."},
		"user_id":      schema.StringAttribute{Computed: true, MarkdownDescription: "Assigned directory-user UUID, or null when unassigned."},
		"asset_tag":    schema.StringAttribute{Computed: true, MarkdownDescription: "Assigned asset tag, or null when empty."},
	}
	if r.ade {
		attrs["use_blueprint_routing"] = schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the ADE record uses Blueprint routing."}
		attrs["override_dep_profile_id"] = schema.StringAttribute{Computed: true, MarkdownDescription: "ADE Library Item override UUID, or null when cleared or not reported by the API. Management requires the API to report this property."}
	} else {
		attrs["tags"] = schema.SetAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "Assigned tag names."}
	}
	resp.Schema = schema.Schema{MarkdownDescription: "Reads only assignment metadata for one exact device UUID. It omits names, serial numbers, user email/name, inventory, secrets and operational history. Makes no mutations.", Attributes: attrs}
}
func (r *DeviceDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok || c == nil {
		resp.Diagnostics.AddError("Unexpected Iru client type", "Could not configure the device metadata lookup.")
		return
	}
	r.client = c
}
func (r *DeviceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var id types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("device_id"), &id)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil || id.IsNull() || id.IsUnknown() || !client.ValidID(id.ValueString()) {
		resp.Diagnostics.AddError("Invalid device lookup", "Configure the provider and supply an exact known device UUID.")
		return
	}
	current, err := r.client.GetDeviceSettings(ctx, id.ValueString(), r.ade)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Iru device metadata", err.Error())
		return
	}
	prior := emptyDeviceSettingsObject(r.ade)
	attrs := prior.Attributes()
	attrs["device_id"] = id
	prior = types.ObjectValueMust(prior.AttributeTypes(ctx), attrs)
	value, diags := deviceSettingsState(ctx, prior, current, r.ade)
	resp.Diagnostics.Append(diags...)
	if !resp.Diagnostics.HasError() {
		resp.Diagnostics.Append(resp.State.Set(ctx, value)...)
	}
}
