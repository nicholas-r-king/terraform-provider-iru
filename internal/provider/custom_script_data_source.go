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

type CustomScriptDataSource struct{ client *client.Client }

type scriptLookupModel struct {
	LibraryItemID          types.String `tfsdk:"library_item_id"`
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

var (
	_ datasource.DataSource                   = &CustomScriptDataSource{}
	_ datasource.DataSourceWithConfigure      = &CustomScriptDataSource{}
	_ datasource.DataSourceWithValidateConfig = &CustomScriptDataSource{}
)

func NewCustomScriptDataSource() datasource.DataSource { return &CustomScriptDataSource{} }

func (*CustomScriptDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_custom_script"
}

func (*CustomScriptDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Reads one exact Custom Script Library Item through GET only. Reports current script/remediation bytes, execution settings and optional Self Service metadata without defaults or normalization. Missing/null documented core fields are errors; optional missing/null metadata remains null. Does not upload, assign, activate or execute scripts. The single-object endpoint is not paginated.", Attributes: map[string]schema.Attribute{
		"library_item_id":          schema.StringAttribute{Required: true, MarkdownDescription: "Exact existing Custom Script Library Item UUID. Names and partial IDs are not accepted."},
		"id":                       schema.StringAttribute{Computed: true, MarkdownDescription: "Canonical Custom Script UUID."},
		"name":                     schema.StringAttribute{Computed: true, MarkdownDescription: "Current Library Item display name."},
		"script":                   schema.StringAttribute{Computed: true, Sensitive: true, MarkdownDescription: "Exact script bytes reported by Iru, including meaningful whitespace. Sensitive in displays but stored in Terraform state."},
		"remediation_script":       schema.StringAttribute{Computed: true, Sensitive: true, MarkdownDescription: "Exact remediation script bytes reported by Iru. A returned empty string remains empty. Sensitive in displays but stored in Terraform state."},
		"execution_frequency":      schema.StringAttribute{Computed: true, MarkdownDescription: "Observed once, every_15_min, every_day or no_enforcement setting; reading never schedules execution."},
		"active":                   schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether Iru reports the Library Item as active. Reading never activates it."},
		"restart":                  schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether Iru reports restart enabled. Reading never restarts a device."},
		"show_in_self_service":     schema.BoolAttribute{Computed: true, MarkdownDescription: "Observed Self Service visibility; null when omitted/null in the response, distinct from false."},
		"self_service_category_id": schema.StringAttribute{Computed: true, MarkdownDescription: "Reported Self Service category UUID, or null when omitted/null. Preserved even when visibility is false."},
		"self_service_recommended": schema.BoolAttribute{Computed: true, MarkdownDescription: "Reported recommendation flag, or null when omitted/null, distinct from false. Preserved even when visibility is false."},
	}}
}

func (d *CustomScriptDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok || c == nil {
		resp.Diagnostics.AddError("Unexpected Iru client type", "Could not configure the Custom Script configuration lookup.")
		return
	}
	d.client = c
}

func (*CustomScriptDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	var id types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("library_item_id"), &id)...)
	if resp.Diagnostics.HasError() || id.IsNull() || id.IsUnknown() {
		return
	}
	if !client.ValidID(id.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("library_item_id"), "Invalid Custom Script UUID", "Supply the exact existing Library Item UUID, not a name or partial ID.")
	}
}

func (d *CustomScriptDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var id types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("library_item_id"), &id)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if d.client == nil || id.IsNull() || id.IsUnknown() || !client.ValidID(id.ValueString()) {
		resp.Diagnostics.AddError("Custom Script lookup unavailable", "Configure the provider and supply an exact known Library Item UUID.")
		return
	}
	current, err := d.client.GetScriptConfiguration(ctx, id.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Iru Custom Script configuration", err.Error())
		return
	}
	// Observe returned bytes and optional metadata, not resource source-equivalence rules.
	data := scriptLookupModel{
		LibraryItemID: id, ID: types.StringValue(current.ID), Name: types.StringValue(current.Name),
		Script: types.StringValue(current.Script), RemediationScript: types.StringValue(current.RemediationScript),
		ExecutionFrequency: types.StringValue(current.ExecutionFrequency), Active: types.BoolValue(current.Active), Restart: types.BoolValue(current.Restart),
		ShowInSelfService: types.BoolPointerValue(current.ShowInSelfService), SelfServiceCategoryID: types.StringPointerValue(current.SelfServiceCategoryID), SelfServiceRecommended: types.BoolPointerValue(current.SelfServiceRecommended),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
