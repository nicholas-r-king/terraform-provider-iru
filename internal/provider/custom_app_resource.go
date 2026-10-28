// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
)

var (
	_ resource.Resource                   = &CustomAppResource{}
	_ resource.ResourceWithConfigure      = &CustomAppResource{}
	_ resource.ResourceWithImportState    = &CustomAppResource{}
	_ resource.ResourceWithValidateConfig = &CustomAppResource{}
)

type CustomAppResource struct{ client *client.Client }

type appModel struct {
	ID                     types.String `tfsdk:"id"`
	Name                   types.String `tfsdk:"name"`
	FileKey                types.String `tfsdk:"file_key"`
	InstallType            types.String `tfsdk:"install_type"`
	InstallEnforcement     types.String `tfsdk:"install_enforcement"`
	AuditScript            types.String `tfsdk:"audit_script"`
	PreinstallScript       types.String `tfsdk:"preinstall_script"`
	PostinstallScript      types.String `tfsdk:"postinstall_script"`
	UnzipLocation          types.String `tfsdk:"unzip_location"`
	Active                 types.Bool   `tfsdk:"active"`
	Restart                types.Bool   `tfsdk:"restart"`
	ShowInSelfService      types.Bool   `tfsdk:"show_in_self_service"`
	SelfServiceCategoryID  types.String `tfsdk:"self_service_category_id"`
	SelfServiceRecommended types.Bool   `tfsdk:"self_service_recommended"`
	SHA256                 types.String `tfsdk:"sha256"`
	FileSize               types.Int64  `tfsdk:"file_size"`
}

func NewCustomAppResource() resource.Resource { return &CustomAppResource{} }

func (*CustomAppResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_custom_app"
}

func (*CustomAppResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages one Custom App Library Item referencing an existing uploaded installer file_key. Does not upload or delete S3 artifacts, expose signed URLs, or manage assignments. Import by Library Item UUID.",
		Attributes: map[string]schema.Attribute{
			"id":                       schema.StringAttribute{Computed: true, MarkdownDescription: "Library Item UUID.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name":                     schema.StringAttribute{Required: true, MarkdownDescription: "Display name."},
			"file_key":                 schema.StringAttribute{Required: true, MarkdownDescription: "Existing installer key from the documented Iru upload workflow. Upload must have completed. Not a URL or local path."},
			"install_type":             schema.StringAttribute{Required: true, MarkdownDescription: "package, zip, or image. zip requires unzip_location."},
			"install_enforcement":      schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("install_once"), MarkdownDescription: "install_once, continuously_enforce, or no_enforcement. Continuous enforcement requires audit_script; no enforcement requires Self Service and a category."},
			"audit_script":             schema.StringAttribute{Optional: true, Computed: true, Sensitive: true, Default: stringdefault.StaticString(""), MarkdownDescription: "Audit script for continuously_enforce only. Content remains in state."},
			"preinstall_script":        schema.StringAttribute{Optional: true, Computed: true, Sensitive: true, Default: stringdefault.StaticString(""), MarkdownDescription: "Preinstall script; empty string clears it. Content remains in state."},
			"postinstall_script":       schema.StringAttribute{Optional: true, Computed: true, Sensitive: true, Default: stringdefault.StaticString(""), MarkdownDescription: "Postinstall script; empty string clears it. Content remains in state."},
			"unzip_location":           schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString(""), MarkdownDescription: "Extraction destination required for zip installers."},
			"active":                   schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), MarkdownDescription: "Whether the item is active and installable. Defaults to false."},
			"restart":                  schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), MarkdownDescription: "Restart after installation. Defaults to false."},
			"show_in_self_service":     schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), MarkdownDescription: "Display in Self Service; requires an existing category UUID."},
			"self_service_category_id": schema.StringAttribute{Optional: true, MarkdownDescription: "Existing category UUID, managed only while Self Service is enabled."},
			"self_service_recommended": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), MarkdownDescription: "Recommend in Self Service; requires Self Service."},
			"sha256":                   schema.StringAttribute{Computed: true, MarkdownDescription: "Installer SHA256 reported by Iru, or null when absent. Never inferred from file_key. This provider does not hash or verify local installer bytes."},
			"file_size":                schema.Int64Attribute{Computed: true, MarkdownDescription: "Installer size reported by Iru, or null when absent."},
		},
	}
}

func (r *CustomAppResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok || c == nil {
		resp.Diagnostics.AddError("Unexpected Iru client type", "The provider could not configure its Custom Apps client.")
		return
	}
	r.client = c
}

func (data appModel) unknownInput() bool {
	for _, value := range []types.String{data.Name, data.FileKey, data.InstallType, data.InstallEnforcement, data.AuditScript, data.PreinstallScript, data.PostinstallScript, data.UnzipLocation, data.SelfServiceCategoryID} {
		if value.IsUnknown() {
			return true
		}
	}
	return data.Active.IsUnknown() || data.Restart.IsUnknown() || data.ShowInSelfService.IsUnknown() || data.SelfServiceRecommended.IsUnknown()
}

func (data appModel) input() client.AppInput {
	frequency := data.InstallEnforcement.ValueString()
	if data.InstallEnforcement.IsNull() {
		frequency = "install_once"
	}
	input := client.AppInput{Name: data.Name.ValueString(), FileKey: data.FileKey.ValueString(), InstallType: data.InstallType.ValueString(), InstallEnforcement: frequency,
		AuditScript: data.AuditScript.ValueString(), PreinstallScript: data.PreinstallScript.ValueString(), PostinstallScript: data.PostinstallScript.ValueString(), UnzipLocation: data.UnzipLocation.ValueString(),
		Active: data.Active.ValueBool(), Restart: data.Restart.ValueBool(), ShowInSelfService: data.ShowInSelfService.ValueBool()}
	if !data.SelfServiceCategoryID.IsNull() && !data.SelfServiceCategoryID.IsUnknown() {
		value := data.SelfServiceCategoryID.ValueString()
		input.SelfServiceCategoryID = &value
	}
	if input.ShowInSelfService || data.SelfServiceRecommended.ValueBool() {
		value := data.SelfServiceRecommended.ValueBool()
		input.SelfServiceRecommended = &value
	}
	return input
}

func (*CustomAppResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data appModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || data.unknownInput() {
		return // Resolved inputs are checked again before every write.
	}
	if err := data.input().Validate(); err != nil {
		resp.Diagnostics.AddError("Invalid Custom App configuration", err.Error())
	}
}

func appState(app client.App, prior appModel) appModel {
	data := appModel{ID: types.StringValue(app.ID), Name: types.StringValue(app.Name), FileKey: types.StringValue(app.FileKey), InstallType: types.StringValue(app.InstallType), InstallEnforcement: types.StringValue(app.InstallEnforcement),
		AuditScript: scriptValue(app.AuditScript, prior.AuditScript), PreinstallScript: scriptValue(app.PreinstallScript, prior.PreinstallScript), PostinstallScript: scriptValue(app.PostinstallScript, prior.PostinstallScript), UnzipLocation: types.StringValue(app.UnzipLocation),
		Active: types.BoolValue(app.Active), Restart: types.BoolValue(app.Restart), ShowInSelfService: types.BoolValue(app.ShowInSelfService), SelfServiceCategoryID: types.StringNull(), SelfServiceRecommended: types.BoolValue(false), SHA256: types.StringNull(), FileSize: types.Int64Null()}
	if app.SelfServiceCategoryID != nil {
		value := *app.SelfServiceCategoryID
		if !prior.SelfServiceCategoryID.IsUnknown() && !prior.SelfServiceCategoryID.IsNull() && strings.EqualFold(value, prior.SelfServiceCategoryID.ValueString()) {
			value = prior.SelfServiceCategoryID.ValueString()
		}
		data.SelfServiceCategoryID = types.StringValue(value)
	}
	if app.SelfServiceRecommended != nil {
		data.SelfServiceRecommended = types.BoolValue(*app.SelfServiceRecommended)
	}
	if app.SHA256 != nil {
		data.SHA256 = types.StringValue(*app.SHA256)
	}
	if app.FileSize != nil {
		data.FileSize = types.Int64Value(*app.FileSize)
	}
	return data
}

func (r *CustomAppResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data appModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil || data.unknownInput() {
		resp.Diagnostics.AddError("Custom App client or plan unavailable", "Configure the provider and resolve all app inputs before creation.")
		return
	}
	created, err := r.client.CreateApp(ctx, data.input())
	if err != nil {
		if client.ValidID(created.ID) {
			data.ID = types.StringValue(strings.ToLower(created.ID))
			// Computed metadata is unknown in a real create plan. Partial state
			// must contain concrete values so Terraform can retain the new ID.
			data.SHA256, data.FileSize = types.StringNull(), types.Int64Null()
			resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
		}
		resp.Diagnostics.AddError("Unable to create Iru Custom App", err.Error()+" The installer may still be processing. Inspect/import any created item before retrying; writes are not replayed.")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, appState(created, data))...)
}

func (r *CustomAppResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data appModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Iru client unavailable", "Configure the provider before reading a Custom App.")
		return
	}
	app, err := r.client.GetApp(ctx, data.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Iru Custom App", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, appState(app, data))...)
}

func (r *CustomAppResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data appModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil || data.unknownInput() {
		resp.Diagnostics.AddError("Custom App client or plan unavailable", "Configure the provider and resolve all app inputs before update.")
		return
	}
	var prior appModel
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	app, err := r.client.UpdateApp(ctx, data.ID.ValueString(), data.input(), data.FileKey.ValueString() != prior.FileKey.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to update Iru Custom App", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, appState(app, data))...)
}

func (r *CustomAppResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data appModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Iru client unavailable", "Configure the provider before deleting a Custom App.")
		return
	}
	if err := r.client.DeleteApp(ctx, data.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Unable to delete Iru Custom App", err.Error())
	}
}

func (*CustomAppResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if !client.ValidID(req.ID) {
		resp.Diagnostics.AddError("Invalid Custom App import ID", "Import using the Library Item UUID.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), strings.ToLower(req.ID))...)
}
