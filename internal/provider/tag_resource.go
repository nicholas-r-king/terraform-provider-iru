// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
)

type TagResource struct{ client *client.Client }
type tagModel struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
}

var (
	_ resource.Resource                   = &TagResource{}
	_ resource.ResourceWithConfigure      = &TagResource{}
	_ resource.ResourceWithImportState    = &TagResource{}
	_ resource.ResourceWithValidateConfig = &TagResource{}
)

func NewTagResource() resource.Resource { return &TagResource{} }
func (*TagResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_tag"
}
func (*TagResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Manages one Iru Tag. Import by UUID. Device associations are not managed by this resource. Deletion permanently removes the Tag.", Attributes: map[string]schema.Attribute{
		"id":   schema.StringAttribute{Computed: true, MarkdownDescription: "Tag UUID.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"name": schema.StringAttribute{Required: true, MarkdownDescription: "Tag name."},
	}}
}
func (r *TagResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok || c == nil {
		resp.Diagnostics.AddError("Unexpected Iru client type", "The provider could not configure its Tag client.")
		return
	}
	r.client = c
}
func (*TagResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data tagModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if !data.Name.IsUnknown() && !data.Name.IsNull() && strings.TrimSpace(data.Name.ValueString()) == "" {
		resp.Diagnostics.AddAttributeError(path.Root("name"), "Invalid Tag name", "name must not be blank.")
	}
}
func (r *TagResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data tagModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil || data.Name.IsUnknown() {
		resp.Diagnostics.AddError("Tag client or plan unavailable", "Configure the provider and resolve name before creation.")
		return
	}
	value, err := r.client.CreateTag(ctx, data.Name.ValueString())
	if err != nil {
		if client.ValidID(value.ID) {
			data.ID = types.StringValue(strings.ToLower(value.ID))
			resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
		}
		resp.Diagnostics.AddError("Unable to create Iru Tag", err.Error()+" Inspect/import a potentially created Tag before retrying.")
		return
	}
	data.ID, data.Name = types.StringValue(value.ID), types.StringValue(value.Name)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
func (r *TagResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data tagModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Iru client unavailable", "Configure the provider before reading a Tag.")
		return
	}
	value, err := r.client.GetTag(ctx, data.ID.ValueString())
	if client.IsTagAbsent(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Iru Tag", err.Error())
		return
	}
	data.ID, data.Name = types.StringValue(value.ID), types.StringValue(value.Name)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
func (r *TagResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data tagModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil || data.Name.IsUnknown() {
		resp.Diagnostics.AddError("Tag client or plan unavailable", "Configure the provider and resolve name before update.")
		return
	}
	value, err := r.client.UpdateTag(ctx, data.ID.ValueString(), data.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to update Iru Tag", err.Error())
		return
	}
	data.ID, data.Name = types.StringValue(value.ID), types.StringValue(value.Name)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
func (r *TagResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data tagModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Iru client unavailable", "Configure the provider before deleting a Tag.")
		return
	}
	if err := r.client.DeleteTag(ctx, data.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Unable to delete Iru Tag", err.Error())
	}
}
func (*TagResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if !client.ValidID(req.ID) {
		resp.Diagnostics.AddError("Invalid Tag import ID", "Import using the Tag UUID.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), strings.ToLower(req.ID))...)
}
