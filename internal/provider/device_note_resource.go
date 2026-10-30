// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
	"strings"
)

type DeviceNoteResource struct{ client *client.Client }
type deviceNoteModel struct {
	ID       types.String `tfsdk:"id"`
	DeviceID types.String `tfsdk:"device_id"`
	Content  types.String `tfsdk:"content"`
}

var (
	_ resource.Resource                   = &DeviceNoteResource{}
	_ resource.ResourceWithConfigure      = &DeviceNoteResource{}
	_ resource.ResourceWithImportState    = &DeviceNoteResource{}
	_ resource.ResourceWithValidateConfig = &DeviceNoteResource{}
)

func NewDeviceNoteResource() resource.Resource { return &DeviceNoteResource{} }
func (*DeviceNoteResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_device_note"
}
func (*DeviceNoteResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Manages a Note on an existing enrolled device. This resource never enrolls, deletes, erases or reconfigures the device. Import using device_uuid/note_uuid.", Attributes: map[string]schema.Attribute{
		"id":        schema.StringAttribute{Computed: true, MarkdownDescription: "Note UUID.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"device_id": schema.StringAttribute{Required: true, MarkdownDescription: "Existing device UUID. Moving the Note to a different device requires replacement.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"content":   schema.StringAttribute{Required: true, Sensitive: true, MarkdownDescription: "Note content as returned by Iru, including HTML when present. Stored in Terraform state; avoid embedding credentials."},
	}}
}
func (r *DeviceNoteResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok || c == nil {
		resp.Diagnostics.AddError("Unexpected Iru client type", "Could not configure the Device Note client.")
		return
	}
	r.client = c
}
func (*DeviceNoteResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data deviceNoteModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !data.DeviceID.IsUnknown() && !data.DeviceID.IsNull() && !client.ValidID(data.DeviceID.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("device_id"), "Invalid device ID", "device_id must be a UUID.")
	}
	if !data.Content.IsUnknown() && !data.Content.IsNull() && strings.TrimSpace(data.Content.ValueString()) == "" {
		resp.Diagnostics.AddAttributeError(path.Root("content"), "Invalid Note content", "content must not be blank.")
	}
}
func (r *DeviceNoteResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data deviceNoteModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil || data.DeviceID.IsUnknown() || data.Content.IsUnknown() {
		resp.Diagnostics.AddError("Device Note client or plan unavailable", "Configure the provider and resolve the device and content before creation.")
		return
	}
	value, err := r.client.CreateDeviceNote(ctx, data.DeviceID.ValueString(), data.Content.ValueString())
	if err != nil {
		if client.ValidID(value.ID) {
			data.ID = types.StringValue(strings.ToLower(value.ID))
			resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
		}
		resp.Diagnostics.AddError("Unable to create Iru Device Note", err.Error()+" Inspect/import a potentially created Note before retrying.")
		return
	}
	data.ID, data.Content = types.StringValue(value.ID), types.StringValue(value.Content)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
func (r *DeviceNoteResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data deviceNoteModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Iru client unavailable", "Configure the provider before reading a Device Note.")
		return
	}
	value, err := r.client.GetDeviceNote(ctx, data.DeviceID.ValueString(), data.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Iru Device Note", err.Error())
		return
	}
	data.Content = types.StringValue(value.Content)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
func (r *DeviceNoteResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data deviceNoteModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil || data.DeviceID.IsUnknown() || data.Content.IsUnknown() {
		resp.Diagnostics.AddError("Device Note client or plan unavailable", "Configure the provider and resolve the device and content before update.")
		return
	}
	value, err := r.client.UpdateDeviceNote(ctx, data.DeviceID.ValueString(), data.ID.ValueString(), data.Content.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to update Iru Device Note", err.Error())
		return
	}
	data.Content = types.StringValue(value.Content)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
func (r *DeviceNoteResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data deviceNoteModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Iru client unavailable", "Configure the provider before deleting a Device Note.")
		return
	}
	if err := r.client.DeleteDeviceNote(ctx, data.DeviceID.ValueString(), data.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Unable to delete Iru Device Note", err.Error())
	}
}
func (*DeviceNoteResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	device, note, found := strings.Cut(req.ID, "/")
	if !found || !client.ValidID(device) || !client.ValidID(note) {
		resp.Diagnostics.AddError("Invalid Device Note import ID", "Import using device_uuid/note_uuid.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), strings.ToLower(note))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("device_id"), strings.ToLower(device))...)
}
