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
	_ resource.Resource                   = &CustomScriptResource{}
	_ resource.ResourceWithConfigure      = &CustomScriptResource{}
	_ resource.ResourceWithImportState    = &CustomScriptResource{}
	_ resource.ResourceWithValidateConfig = &CustomScriptResource{}
	_ resource.ResourceWithModifyPlan     = &CustomScriptResource{}
)

type CustomScriptResource struct{ client *client.Client }

type scriptModel struct {
	ID                     types.String `tfsdk:"id"`
	Name                   types.String `tfsdk:"name"`
	Script                 types.String `tfsdk:"script"`
	RemediationScript      types.String `tfsdk:"remediation_script"`
	ExecutionFrequency     types.String `tfsdk:"execution_frequency"`
	Active                 types.Bool   `tfsdk:"active"`
	Restart                types.Bool   `tfsdk:"restart"`
	ShowInSelfService      types.Bool   `tfsdk:"show_in_self_service"`
	SelfServiceCategoryID  types.String `tfsdk:"self_service_category_id"`
	SelfServiceRecommended types.Bool   `tfsdk:"self_service_recommended"`
}

func NewCustomScriptResource() resource.Resource { return &CustomScriptResource{} }

func (*CustomScriptResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_custom_script"
}

func (*CustomScriptResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages one Iru Custom Script, including audit/remediation content and Self Service settings. Import by Library Item UUID. No blueprint assignments are managed.",
		Attributes: map[string]schema.Attribute{
			"id":                       schema.StringAttribute{Computed: true, MarkdownDescription: "Library Item UUID.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name":                     schema.StringAttribute{Required: true, MarkdownDescription: "Display name."},
			"script":                   schema.StringAttribute{Required: true, Sensitive: true, MarkdownDescription: "Audit script content. Stored in Terraform state; do not embed secrets."},
			"remediation_script":       schema.StringAttribute{Optional: true, Computed: true, Sensitive: true, Default: stringdefault.StaticString(""), MarkdownDescription: "Remediation content. Omitted on creation when absent; clearing existing content is currently unsupported. Stored in Terraform state."},
			"execution_frequency":      schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("once"), MarkdownDescription: "once, every_15_min, every_day, or no_enforcement. no_enforcement requires Self Service and a category."},
			"active":                   schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), MarkdownDescription: "Whether the Library Item is active. Defaults to false."},
			"restart":                  schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), MarkdownDescription: "Whether to restart after running. Defaults to false."},
			"show_in_self_service":     schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), MarkdownDescription: "Whether to show in Self Service; requires a category."},
			"self_service_category_id": schema.StringAttribute{Optional: true, MarkdownDescription: "Existing Self Service category UUID. Set only while show_in_self_service is true."},
			"self_service_recommended": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), MarkdownDescription: "Recommend in Self Service; true requires show_in_self_service."},
		},
	}
}

func (r *CustomScriptResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok || c == nil {
		resp.Diagnostics.AddError("Unexpected Iru client type", "The provider could not configure its Custom Scripts client.")
		return
	}
	r.client = c
}

func (*CustomScriptResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data scriptModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !data.Name.IsNull() && !data.Name.IsUnknown() && strings.TrimSpace(data.Name.ValueString()) == "" {
		resp.Diagnostics.AddAttributeError(path.Root("name"), "Invalid script name", "name must not be empty or whitespace.")
	}
	if !data.Script.IsNull() && !data.Script.IsUnknown() && strings.TrimSpace(data.Script.ValueString()) == "" {
		resp.Diagnostics.AddAttributeError(path.Root("script"), "Invalid script content", "script must not be empty or whitespace.")
	}
	if !data.ExecutionFrequency.IsNull() && !data.ExecutionFrequency.IsUnknown() && !client.ValidFrequency(data.ExecutionFrequency.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("execution_frequency"), "Invalid execution frequency", "Use once, every_15_min, every_day, or no_enforcement.")
	}
	if !data.SelfServiceCategoryID.IsNull() && !data.SelfServiceCategoryID.IsUnknown() && !client.ValidID(data.SelfServiceCategoryID.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("self_service_category_id"), "Invalid category ID", "The category ID must be a UUID.")
	}
	// Unknown dependencies are rechecked against resolved values before writes.
	if data.ShowInSelfService.IsUnknown() {
		return
	}
	show := data.ShowInSelfService.ValueBool()
	if !data.ExecutionFrequency.IsUnknown() && data.ExecutionFrequency.ValueString() == "no_enforcement" && !show {
		resp.Diagnostics.AddAttributeError(path.Root("execution_frequency"), "Self Service required", "no_enforcement requires show_in_self_service=true and self_service_category_id.")
	}
	if show && !data.SelfServiceCategoryID.IsUnknown() && data.SelfServiceCategoryID.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("self_service_category_id"), "Self Service category required", "Set an existing category UUID when showing the script in Self Service.")
	}
	if !show && !data.SelfServiceCategoryID.IsUnknown() && !data.SelfServiceCategoryID.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("self_service_category_id"), "Self Service is disabled", "This provider manages a category only while show_in_self_service=true.")
	}
	if !show && !data.SelfServiceRecommended.IsUnknown() && data.SelfServiceRecommended.ValueBool() {
		resp.Diagnostics.AddAttributeError(path.Root("self_service_recommended"), "Self Service is disabled", "Recommendations require show_in_self_service=true.")
	}
}

func (data scriptModel) input() client.ScriptInput {
	input := client.ScriptInput{
		Name: data.Name.ValueString(), Script: data.Script.ValueString(), RemediationScript: data.RemediationScript.ValueString(),
		ExecutionFrequency: data.ExecutionFrequency.ValueString(), Active: data.Active.ValueBool(), Restart: data.Restart.ValueBool(),
		ShowInSelfService: data.ShowInSelfService.ValueBool(),
	}
	if !data.SelfServiceCategoryID.IsNull() && !data.SelfServiceCategoryID.IsUnknown() {
		category := data.SelfServiceCategoryID.ValueString()
		input.SelfServiceCategoryID = &category
	}
	if input.ShowInSelfService || data.SelfServiceRecommended.ValueBool() {
		recommended := data.SelfServiceRecommended.ValueBool()
		input.SelfServiceRecommended = &recommended
	}
	return input
}

func (data scriptModel) hasUnknownInput() bool {
	return data.Name.IsUnknown() || data.Script.IsUnknown() || data.RemediationScript.IsUnknown() ||
		data.ExecutionFrequency.IsUnknown() || data.Active.IsUnknown() || data.Restart.IsUnknown() ||
		data.ShowInSelfService.IsUnknown() || data.SelfServiceCategoryID.IsUnknown() || data.SelfServiceRecommended.IsUnknown()
}

func scriptState(script client.Script, prior scriptModel) scriptModel {
	data := scriptModel{
		ID: types.StringValue(script.ID), Name: types.StringValue(script.Name), Script: scriptValue(script.Script, prior.Script),
		RemediationScript: scriptValue(script.RemediationScript, prior.RemediationScript), ExecutionFrequency: types.StringValue(script.ExecutionFrequency),
		Active: types.BoolValue(script.Active), Restart: types.BoolValue(script.Restart), ShowInSelfService: types.BoolValue(script.ShowInSelfService),
		SelfServiceCategoryID: types.StringNull(), SelfServiceRecommended: types.BoolValue(false),
	}
	if script.SelfServiceCategoryID != nil {
		category := *script.SelfServiceCategoryID
		// UUIDs are case-insensitive. Preserve configured/prior spelling when
		// Iru returns the same category in canonical case, avoiding an
		// inconsistent apply result or perpetual case-only refresh drift.
		if !prior.SelfServiceCategoryID.IsNull() && !prior.SelfServiceCategoryID.IsUnknown() &&
			strings.EqualFold(category, prior.SelfServiceCategoryID.ValueString()) {
			category = prior.SelfServiceCategoryID.ValueString()
		}
		data.SelfServiceCategoryID = types.StringValue(category)
	}
	if script.SelfServiceRecommended != nil {
		data.SelfServiceRecommended = types.BoolValue(*script.SelfServiceRecommended)
	}
	return data
}

func (r *CustomScriptResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data scriptModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if data.hasUnknownInput() {
		resp.Diagnostics.AddError("Unresolved Custom Script plan", "All script inputs must be known before creation.")
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Iru client unavailable", "Configure the Iru provider before creating a script.")
		return
	}
	created, err := r.client.CreateScript(ctx, data.input())
	if err != nil {
		if client.ValidID(created.ID) {
			data.ID = types.StringValue(strings.ToLower(created.ID))
			resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
		}
		resp.Diagnostics.AddError("Unable to create Iru Custom Script", err.Error()+" If the request reached Iru, inspect/import the item before retrying.")
		return
	}
	data = scriptState(created, data)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *CustomScriptResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data scriptModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Iru client unavailable", "Configure the Iru provider before reading a script.")
		return
	}
	script, err := r.client.GetScript(ctx, data.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Iru Custom Script", err.Error())
		return
	}
	data = scriptState(script, data)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *CustomScriptResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data scriptModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if data.hasUnknownInput() {
		resp.Diagnostics.AddError("Unresolved Custom Script plan", "All script inputs must be known before update.")
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Iru client unavailable", "Configure the Iru provider before updating a script.")
		return
	}
	var prior scriptModel
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	updated, err := r.client.UpdateScript(ctx, data.ID.ValueString(), data.input(), prior.input())
	if err != nil {
		resp.Diagnostics.AddError("Unable to update Iru Custom Script", err.Error())
		return
	}
	data = scriptState(updated, data)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *CustomScriptResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data scriptModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Iru client unavailable", "Configure the Iru provider before deleting a script.")
		return
	}
	if err := r.client.DeleteScript(ctx, data.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Unable to delete Iru Custom Script", err.Error())
	}
}

func (*CustomScriptResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if !client.ValidID(req.ID) {
		resp.Diagnostics.AddError("Invalid Custom Script import ID", "Import using the Library Item UUID. No names, paths, or composite IDs are accepted.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), strings.ToLower(req.ID))...)
}

func (*CustomScriptResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var before, after scriptModel
	resp.Diagnostics.Append(req.State.Get(ctx, &before)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &after)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !before.RemediationScript.IsUnknown() && before.RemediationScript.ValueString() != "" && !after.RemediationScript.IsUnknown() && after.RemediationScript.ValueString() == "" {
		resp.Diagnostics.AddError("Remediation clearing unsupported", client.RemediationClearUnsupported)
	}
}
