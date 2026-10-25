// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
)

var (
	_ datasource.DataSource              = &BlueprintDataSource{}
	_ datasource.DataSourceWithConfigure = &BlueprintDataSource{}
	_ datasource.DataSource              = &LibraryItemDataSource{}
	_ datasource.DataSourceWithConfigure = &LibraryItemDataSource{}
)

type BlueprintDataSource struct{ client *client.Client }
type LibraryItemDataSource struct{ client *client.Client }

type blueprintLookupModel struct {
	BlueprintID types.String `tfsdk:"blueprint_id"`
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Type        types.String `tfsdk:"type"`
}

type libraryItemLookupModel struct {
	BlueprintID   types.String `tfsdk:"blueprint_id"`
	LibraryItemID types.String `tfsdk:"library_item_id"`
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
}

func NewBlueprintDataSource() datasource.DataSource   { return &BlueprintDataSource{} }
func NewLibraryItemDataSource() datasource.DataSource { return &LibraryItemDataSource{} }

func (*BlueprintDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_blueprint"
}

func (*LibraryItemDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_library_item"
}

func (*BlueprintDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Reads one existing Blueprint by exact UUID. Exposes only ID, name and type; no enrollment codes, settings or device inventory.", Attributes: map[string]schema.Attribute{
		"blueprint_id": schema.StringAttribute{Required: true, MarkdownDescription: "Existing Blueprint UUID. Names and partial matches are not accepted."},
		"id":           schema.StringAttribute{Computed: true, MarkdownDescription: "Canonical Blueprint UUID."},
		"name":         schema.StringAttribute{Computed: true, MarkdownDescription: "Blueprint display name."},
		"type":         schema.StringAttribute{Computed: true, MarkdownDescription: "Blueprint type reported by Iru, such as classic or map."},
	}}
}

func (*LibraryItemDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Reads one existing Library Item's membership metadata in a Blueprint by exact UUID. Does not expose assignment-node placement, mutate membership, or collect device/status/activity data.", Attributes: map[string]schema.Attribute{
		"blueprint_id":    schema.StringAttribute{Required: true, MarkdownDescription: "Existing Blueprint UUID whose Library Item list is searched."},
		"library_item_id": schema.StringAttribute{Required: true, MarkdownDescription: "Exact Library Item UUID expected in that Blueprint."},
		"id":              schema.StringAttribute{Computed: true, MarkdownDescription: "Canonical Library Item UUID."},
		"name":            schema.StringAttribute{Computed: true, MarkdownDescription: "Library Item display name."},
	}}
}

func (d *BlueprintDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok || c == nil {
		resp.Diagnostics.AddError("Unexpected Iru client type", "The provider could not configure its Blueprint lookup.")
		return
	}
	d.client = c
}

func (d *LibraryItemDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok || c == nil {
		resp.Diagnostics.AddError("Unexpected Iru client type", "The provider could not configure its Library Item lookup.")
		return
	}
	d.client = c
}

func (d *BlueprintDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data blueprintLookupModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if d.client == nil || data.BlueprintID.IsUnknown() || !client.ValidID(data.BlueprintID.ValueString()) {
		resp.Diagnostics.AddError("Blueprint lookup unavailable", "Configure the provider and supply a known Blueprint UUID.")
		return
	}
	result, err := d.client.GetBlueprint(ctx, data.BlueprintID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Iru Blueprint", err.Error())
		return
	}
	data.ID, data.Name, data.Type = types.StringValue(result.ID), types.StringValue(result.Name), types.StringValue(result.Type)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (d *LibraryItemDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data libraryItemLookupModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if d.client == nil || data.BlueprintID.IsUnknown() || data.LibraryItemID.IsUnknown() || !client.ValidID(data.BlueprintID.ValueString()) || !client.ValidID(data.LibraryItemID.ValueString()) {
		resp.Diagnostics.AddError("Library Item lookup unavailable", "Configure the provider and supply known Blueprint and Library Item UUIDs.")
		return
	}
	result, err := d.client.FindLibraryItem(ctx, data.BlueprintID.ValueString(), data.LibraryItemID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Iru Library Item", err.Error())
		return
	}
	data.ID, data.Name = types.StringValue(result.ID), types.StringValue(result.Name)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
