// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
	"strings"
)

type InHouseAppResource struct{ client *client.Client }
type inHouseAppModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	FileKey      types.String `tfsdk:"file_key"`
	Active       types.Bool   `tfsdk:"active"`
	RunsOnIphone types.Bool   `tfsdk:"runs_on_iphone"`
	RunsOnIpad   types.Bool   `tfsdk:"runs_on_ipad"`
	RunsOnTV     types.Bool   `tfsdk:"runs_on_tv"`
}

var (
	_ resource.Resource                   = &InHouseAppResource{}
	_ resource.ResourceWithConfigure      = &InHouseAppResource{}
	_ resource.ResourceWithImportState    = &InHouseAppResource{}
	_ resource.ResourceWithValidateConfig = &InHouseAppResource{}
)

func NewInHouseAppResource() resource.Resource { return &InHouseAppResource{} }
func (*InHouseAppResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_in_house_app"
}
func (*InHouseAppResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Manages an In-House App Library Item referencing an already uploaded and validated IPA file_key. Does not upload installer bytes or manage assignments. Import by Library Item UUID.", Attributes: map[string]schema.Attribute{
		"id":             schema.StringAttribute{Computed: true, MarkdownDescription: "Library Item UUID.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"name":           schema.StringAttribute{Required: true, MarkdownDescription: "Library Item name."},
		"file_key":       schema.StringAttribute{Required: true, MarkdownDescription: "Existing validated IPA upload key. Changing this replaces the installer in place."},
		"active":         schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), MarkdownDescription: "Activate the Library Item. Defaults to false."},
		"runs_on_iphone": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), MarkdownDescription: "Target iPhones. Explicitly select at least one platform."},
		"runs_on_ipad":   schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), MarkdownDescription: "Target iPads."},
		"runs_on_tv":     schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), MarkdownDescription: "Target Apple TVs."},
	}}
}
func (r *InHouseAppResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok || c == nil {
		resp.Diagnostics.AddError("Unexpected Iru client type", "Could not configure the In-House App client.")
		return
	}
	r.client = c
}
func (data inHouseAppModel) input() client.InHouseAppInput {
	return client.InHouseAppInput{Name: data.Name.ValueString(), FileKey: data.FileKey.ValueString(), Active: data.Active.ValueBool(), RunsOnIphone: data.RunsOnIphone.ValueBool(), RunsOnIpad: data.RunsOnIpad.ValueBool(), RunsOnTV: data.RunsOnTV.ValueBool()}
}
func (data inHouseAppModel) unknownInput() bool {
	return data.Name.IsUnknown() || data.FileKey.IsUnknown() || data.Active.IsUnknown() || data.RunsOnIphone.IsUnknown() || data.RunsOnIpad.IsUnknown() || data.RunsOnTV.IsUnknown()
}
func (*InHouseAppResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data inHouseAppModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || data.unknownInput() {
		return
	}
	if err := data.input().Validate(); err != nil {
		resp.Diagnostics.AddError("Invalid In-House App configuration", err.Error())
	}
}
func inHouseAppState(value client.InHouseApp) inHouseAppModel {
	return inHouseAppModel{ID: types.StringValue(value.ID), Name: types.StringValue(value.Name), FileKey: types.StringValue(value.FileKey), Active: types.BoolValue(value.Active), RunsOnIphone: types.BoolValue(value.RunsOnIphone), RunsOnIpad: types.BoolValue(value.RunsOnIpad), RunsOnTV: types.BoolValue(value.RunsOnTV)}
}
func (r *InHouseAppResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data inHouseAppModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil || data.unknownInput() {
		resp.Diagnostics.AddError("In-House App client or plan unavailable", "Configure the provider and resolve app inputs before creation.")
		return
	}
	value, err := r.client.CreateInHouseApp(ctx, data.input())
	if err != nil {
		if client.ValidID(value.ID) {
			data.ID = types.StringValue(strings.ToLower(value.ID))
			resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
		}
		resp.Diagnostics.AddError("Unable to create Iru In-House App", err.Error()+" Inspect/import a potentially created item before retrying.")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, inHouseAppState(value))...)
}
func (r *InHouseAppResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data inHouseAppModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Iru client unavailable", "Configure the provider before reading an In-House App.")
		return
	}
	value, err := r.client.GetInHouseApp(ctx, data.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Iru In-House App", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, inHouseAppState(value))...)
}
func (r *InHouseAppResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data, prior inHouseAppModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil || data.unknownInput() {
		resp.Diagnostics.AddError("In-House App client or plan unavailable", "Configure the provider and resolve app inputs before update.")
		return
	}
	value, err := r.client.UpdateInHouseApp(ctx, data.ID.ValueString(), data.input(), !data.FileKey.Equal(prior.FileKey))
	if err != nil {
		resp.Diagnostics.AddError("Unable to update Iru In-House App", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, inHouseAppState(value))...)
}
func (r *InHouseAppResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data inHouseAppModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Iru client unavailable", "Configure the provider before deleting an In-House App.")
		return
	}
	if err := r.client.DeleteInHouseApp(ctx, data.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Unable to delete Iru In-House App", err.Error())
	}
}
func (*InHouseAppResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if !client.ValidID(req.ID) {
		resp.Diagnostics.AddError("Invalid In-House App import ID", "Import using the Library Item UUID.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), strings.ToLower(req.ID))...)
}
