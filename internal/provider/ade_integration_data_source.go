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

type ADEIntegrationDataSource struct{ client *client.Client }

type adeLookupModel struct {
	IntegrationID       types.String `tfsdk:"integration_id"`
	ID                  types.String `tfsdk:"id"`
	BlueprintID         types.String `tfsdk:"blueprint_id"`
	UseBlueprintRouting types.Bool   `tfsdk:"use_blueprint_routing"`
	Email               types.String `tfsdk:"email"`
	Phone               types.String `tfsdk:"phone"`
	ServerName          types.String `tfsdk:"server_name"`
	AccessTokenExpiry   types.String `tfsdk:"access_token_expiry"`
}

var (
	_ datasource.DataSource                   = &ADEIntegrationDataSource{}
	_ datasource.DataSourceWithConfigure      = &ADEIntegrationDataSource{}
	_ datasource.DataSourceWithValidateConfig = &ADEIntegrationDataSource{}
)

func NewADEIntegrationDataSource() datasource.DataSource { return &ADEIntegrationDataSource{} }

func (*ADEIntegrationDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ade_integration"
}

func (*ADEIntegrationDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Reads supported ADE integration metadata for one exact UUID through GET only. Returns the observed default Blueprint/routing assignment, enrollment contact, server name and reported token expiry. Does not expose Apple server tokens, recover the resource's local renewal version, renew tokens, assign devices or create/update/delete integrations. Missing/null required fields and inconsistent assignment metadata fail instead of inventing defaults. This single-object endpoint is not paginated.", Attributes: map[string]schema.Attribute{
		"integration_id":        schema.StringAttribute{Required: true, MarkdownDescription: "Exact existing ADE integration UUID, not an Apple token, server UUID, device UUID or display name."},
		"id":                    schema.StringAttribute{Computed: true, MarkdownDescription: "Canonical ADE integration UUID."},
		"blueprint_id":          schema.StringAttribute{Computed: true, MarkdownDescription: "Observed default Blueprint UUID, or null when the integration explicitly uses Blueprint routing. A missing assignment is an error, not an inferred routing choice."},
		"use_blueprint_routing": schema.BoolAttribute{Computed: true, MarkdownDescription: "Observed routing choice. Legacy nested Blueprint responses imply false; current routing=true requires explicit blueprint_id=null. Reading never changes enrollment assignments."},
		"email":                 schema.StringAttribute{Computed: true, Sensitive: true, MarkdownDescription: "Reported default enrollment contact email, sensitive in displays and stored in state. Preserves explicit empty strings; missing/null is an error."},
		"phone":                 schema.StringAttribute{Computed: true, Sensitive: true, MarkdownDescription: "Reported default enrollment contact phone, sensitive in displays and stored in state. Preserves explicit empty strings; missing/null is an error."},
		"server_name":           schema.StringAttribute{Computed: true, MarkdownDescription: "Apple server display name reported by Iru."},
		"access_token_expiry":   schema.StringAttribute{Computed: true, MarkdownDescription: "Reported expiry string only. Not token bytes, a local renewal version or a guarantee of current token validity/device enrollment."},
	}}
}

func (d *ADEIntegrationDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok || c == nil {
		resp.Diagnostics.AddError("Unexpected Iru client type", "Could not configure the ADE integration configuration lookup.")
		return
	}
	d.client = c
}

func (*ADEIntegrationDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	var id types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("integration_id"), &id)...)
	if resp.Diagnostics.HasError() || id.IsNull() || id.IsUnknown() {
		return
	}
	if !client.ValidID(id.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("integration_id"), "Invalid ADE integration UUID", "Supply the exact existing integration UUID, not a name or partial ID.")
	}
}

func (d *ADEIntegrationDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var id types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("integration_id"), &id)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if d.client == nil || id.IsNull() || id.IsUnknown() || !client.ValidID(id.ValueString()) {
		resp.Diagnostics.AddError("ADE integration lookup unavailable", "Configure the provider and supply an exact known integration UUID.")
		return
	}
	current, err := d.client.GetADEIntegration(ctx, id.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Iru ADE integration configuration", err.Error())
		return
	}
	data := adeLookupModel{
		IntegrationID: id, ID: types.StringValue(current.ID), BlueprintID: types.StringPointerValue(current.BlueprintID), UseBlueprintRouting: types.BoolValue(current.UseBlueprintRouting),
		Email: types.StringPointerValue(current.Defaults.Email), Phone: types.StringPointerValue(current.Defaults.Phone),
		ServerName: types.StringValue(current.ServerName), AccessTokenExpiry: types.StringValue(current.AccessTokenExpiry),
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
