// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"errors"
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
	SourceFile   types.String `tfsdk:"source_file"`
	SourceSHA256 types.String `tfsdk:"source_sha256"`
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
	_ resource.ResourceWithModifyPlan     = &InHouseAppResource{}
)

func NewInHouseAppResource() resource.Resource { return &InHouseAppResource{} }
func (*InHouseAppResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_in_house_app"
}
func (*InHouseAppResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Manages an In-House App Library Item using a validated file_key or local IPA source_file/source_sha256. Local uploads must reach Iru's VALIDATED state before Library Item creation/update. Does not manage assignments or delete detached upload artifacts. Import by Library Item UUID.", Attributes: map[string]schema.Attribute{
		"id":             schema.StringAttribute{Computed: true, MarkdownDescription: "Library Item UUID.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"name":           schema.StringAttribute{Required: true, MarkdownDescription: "Library Item name."},
		"file_key":       schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "Existing validated IPA key or the result of a local upload. Configure this or source_file at creation; omit both to adopt an imported key."},
		"source_file":    schema.StringAttribute{Optional: true, MarkdownDescription: "Local IPA path to upload and validate. Requires source_sha256; conflicts with a configured file_key."},
		"source_sha256":  schema.StringAttribute{Optional: true, MarkdownDescription: "Planned IPA SHA256; use filesha256(). Verified locally and against Iru's validated upload metadata."},
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
	return data.Name.IsUnknown() || data.SourceFile.IsUnknown() || data.SourceSHA256.IsUnknown() || data.Active.IsUnknown() || data.RunsOnIphone.IsUnknown() || data.RunsOnIpad.IsUnknown() || data.RunsOnTV.IsUnknown()
}
func (*InHouseAppResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data inHouseAppModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := validateInstallerSource(data.FileKey, data.SourceFile, data.SourceSHA256); err != nil {
		resp.Diagnostics.AddError("Invalid installer source", err.Error())
		return
	}
	if data.unknownInput() {
		return
	}
	input := data.input()
	if data.FileKey.IsNull() || data.FileKey.IsUnknown() {
		input.FileKey = "installer-key-resolved-at-apply"
	}
	if err := input.Validate(); err != nil {
		resp.Diagnostics.AddError("Invalid In-House App configuration", err.Error())
	}
}
func (*InHouseAppResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	preserveInstallerKey(ctx, req, resp)
}
func inHouseAppState(value client.InHouseApp, prior inHouseAppModel) inHouseAppModel {
	return inHouseAppModel{ID: types.StringValue(value.ID), Name: types.StringValue(value.Name), FileKey: types.StringValue(value.FileKey), Active: types.BoolValue(value.Active), RunsOnIphone: types.BoolValue(value.RunsOnIphone), RunsOnIpad: types.BoolValue(value.RunsOnIpad), RunsOnTV: types.BoolValue(value.RunsOnTV), SourceFile: prior.SourceFile, SourceSHA256: sourceChecksumAfterRead(prior.SourceFile, prior.SourceSHA256, prior.FileKey, value.FileKey, value.SHA256)}
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
	if !data.SourceFile.IsNull() {
		input := data.input()
		input.FileKey = "installer-upload-pending"
		if err := input.Validate(); err != nil {
			resp.Diagnostics.AddError("Invalid In-House App inputs", err.Error())
			return
		}
		upload, err := r.client.UploadInstaller(ctx, "in-house-app", data.SourceFile.ValueString(), data.SourceSHA256.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Unable to upload In-House App installer", err.Error()+" No Library Item create was attempted.")
			return
		}
		data.FileKey = types.StringValue(upload.FileKey)
	}
	value, err := r.client.CreateInHouseApp(ctx, data.input())
	if err == nil && !uploadedInstallerMatches(data.SourceFile, data.SourceSHA256, data.FileKey.ValueString(), value.FileKey, value.SHA256) {
		err = errors.New("Iru In-House App installer metadata does not match the uploaded source")
	}
	if err != nil {
		if client.ValidID(value.ID) {
			data.ID = types.StringValue(strings.ToLower(value.ID))
			resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
		}
		resp.Diagnostics.AddError("Unable to create Iru In-House App", err.Error()+" Inspect/import a potentially created item before retrying.")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, inHouseAppState(value, data))...)
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
	resp.Diagnostics.Append(resp.State.Set(ctx, inHouseAppState(value, data))...)
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
	if !data.SourceFile.IsNull() && !sameSourceChecksum(data.SourceSHA256, prior.SourceSHA256) {
		input := data.input()
		input.FileKey = "installer-upload-pending"
		if err := input.Validate(); err != nil {
			resp.Diagnostics.AddError("Invalid In-House App inputs", err.Error())
			return
		}
		upload, err := r.client.UploadInstaller(ctx, "in-house-app", data.SourceFile.ValueString(), data.SourceSHA256.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Unable to upload In-House App installer", err.Error()+" No Library Item update was attempted.")
			return
		}
		data.FileKey = types.StringValue(upload.FileKey)
	}
	if data.FileKey.IsUnknown() || data.FileKey.IsNull() {
		resp.Diagnostics.AddError("Unresolved In-House App installer", "Resolve the installer key or local upload before updating the Library Item.")
		return
	}
	value, err := r.client.UpdateInHouseApp(ctx, data.ID.ValueString(), data.input(), !data.FileKey.Equal(prior.FileKey))
	if err == nil && !uploadedInstallerMatches(data.SourceFile, data.SourceSHA256, data.FileKey.ValueString(), value.FileKey, value.SHA256) {
		err = errors.New("Iru In-House App installer metadata does not match the managed source")
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to update Iru In-House App", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, inHouseAppState(value, data))...)
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
