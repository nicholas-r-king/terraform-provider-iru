// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
)

type InventoryDataSource struct{ client *client.Client }
type inventoryModel struct {
	ID    types.String `tfsdk:"id"`
	Kind  types.String `tfsdk:"kind"`
	Items types.List   `tfsdk:"items"`
}
type inventoryItemModel struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
}

func NewInventoryDataSource() datasource.DataSource { return &InventoryDataSource{} }
func (*InventoryDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_inventory"
}
func (*InventoryDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Lists ID/name metadata for a documented configuration object family. Useful for migration inventory and selecting import IDs. Never returns scripts, XML, enrollment codes, signed file URLs, device inventory or users. Reads every page with origin/endpoint validation and bounded pagination; results are sorted by UUID.", Attributes: map[string]schema.Attribute{
		"id":   schema.StringAttribute{Computed: true, MarkdownDescription: "Selected inventory kind."},
		"kind": schema.StringAttribute{Required: true, MarkdownDescription: "blueprints, tags, custom_scripts, custom_apps, custom_profiles, in_house_apps, ade_integrations, or self_service_categories."},
		"items": schema.ListNestedAttribute{Computed: true, MarkdownDescription: "Complete UUID/name projection sorted by UUID.", NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
			"id":   schema.StringAttribute{Computed: true, MarkdownDescription: "Object UUID."},
			"name": schema.StringAttribute{Computed: true, MarkdownDescription: "Object display name; ADE uses server_name."},
		}}},
	}}
}
func (r *InventoryDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok || c == nil {
		resp.Diagnostics.AddError("Unexpected Iru client type", "Could not configure the configuration inventory client.")
		return
	}
	r.client = c
}
func (r *InventoryDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data inventoryModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil || data.Kind.IsUnknown() || !client.ValidInventoryKind(data.Kind.ValueString()) {
		resp.Diagnostics.AddError("Invalid configuration inventory request", "Configure the provider and select a known supported inventory kind.")
		return
	}
	items, err := r.client.ListInventory(ctx, data.Kind.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Iru configuration inventory", err.Error())
		return
	}
	values := make([]inventoryItemModel, 0, len(items))
	for _, item := range items {
		values = append(values, inventoryItemModel{ID: types.StringValue(item.ID), Name: types.StringValue(item.Name)})
	}
	list, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: map[string]attr.Type{"id": types.StringType, "name": types.StringType}}, values)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	data.ID, data.Items = data.Kind, list
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
