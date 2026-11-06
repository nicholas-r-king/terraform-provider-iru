// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
)

type LibraryItemStatusesDataSource struct{ client *client.Client }
type libraryItemStatusesModel struct {
	LibraryItemID types.String `tfsdk:"library_item_id"`
	ID            types.String `tfsdk:"id"`
	Count         types.Int64  `tfsdk:"record_count"`
	Statuses      types.List   `tfsdk:"statuses"`
}
type libraryItemStatusModel struct {
	ID          types.Int64  `tfsdk:"id"`
	DeviceID    types.String `tfsdk:"device_id"`
	BlueprintID types.String `tfsdk:"blueprint_id"`
	Status      types.String `tfsdk:"status"`
	ReportedAt  types.String `tfsdk:"reported_at"`
}

var (
	_ datasource.DataSource                   = &LibraryItemStatusesDataSource{}
	_ datasource.DataSourceWithConfigure      = &LibraryItemStatusesDataSource{}
	_ datasource.DataSourceWithValidateConfig = &LibraryItemStatusesDataSource{}
)

func NewLibraryItemStatusesDataSource() datasource.DataSource {
	return &LibraryItemStatusesDataSource{}
}
func (*LibraryItemStatusesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_library_item_statuses"
}
func (*LibraryItemStatusesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Reads a bounded, complete Library Item status snapshot for one exact UUID through GET only. Reports numeric status-record IDs, device/Blueprint UUIDs, opaque reported status and nullable reported timestamp; rows are sorted by status-record ID. Names, user identity, logs, EDR payloads and installer URLs are excluded. An empty list or status string is not proof of assignment placement, successful installation/execution, convergence or item absence. No action or wait-until-success is performed. Every page must succeed; errors preserve cached state rather than return a partial snapshot. Sensitive rows remain plaintext in state/JSON, so protect those files.", Attributes: map[string]schema.Attribute{
		"library_item_id": schema.StringAttribute{Required: true, MarkdownDescription: "Exact existing Library Item UUID; not a device UUID or display name."},
		"id":              schema.StringAttribute{Computed: true, MarkdownDescription: "Canonical selected Library Item UUID."},
		"record_count":    schema.Int64Attribute{Computed: true, MarkdownDescription: "Number of status records in the validated complete snapshot, not necessarily unique devices."},
		"statuses": schema.ListNestedAttribute{Computed: true, Sensitive: true, MarkdownDescription: "Sensitive status-record projection, sorted by numeric ID. Multiple records for one device are retained. Empty means no records were returned; it is not a readiness assertion.", NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
			"id":           schema.Int64Attribute{Computed: true, MarkdownDescription: "Positive numeric status-record ID; not a Library Item or device ID."},
			"device_id":    schema.StringAttribute{Computed: true, MarkdownDescription: "Canonical UUID from the reported computer record."},
			"blueprint_id": schema.StringAttribute{Computed: true, MarkdownDescription: "Reported canonical Blueprint UUID, or null when absent/null. Not node placement or a managed assignment."},
			"status":       schema.StringAttribute{Computed: true, MarkdownDescription: "Exact nonblank reported status string. Unknown status values are observations, not interpreted success states."},
			"reported_at":  schema.StringAttribute{Computed: true, MarkdownDescription: "Exact reported timestamp string, or null when absent/null. No freshness guarantee; an explicit empty string stays empty."},
		}}},
	}}
}
func (d *LibraryItemStatusesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok || c == nil {
		resp.Diagnostics.AddError("Unexpected Iru client type", "Could not configure the Library Item status client.")
		return
	}
	d.client = c
}
func (*LibraryItemStatusesDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	var id types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("library_item_id"), &id)...)
	if !resp.Diagnostics.HasError() && !id.IsUnknown() && !id.IsNull() && !client.ValidID(id.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("library_item_id"), "Invalid Library Item UUID", "Supply the exact Library Item UUID, not a name, URL or partial ID.")
	}
}
func (d *LibraryItemStatusesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data libraryItemStatusesModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || data.LibraryItemID.IsUnknown() {
		return
	}
	if d.client == nil || data.LibraryItemID.IsNull() || !client.ValidID(data.LibraryItemID.ValueString()) {
		resp.Diagnostics.AddError("Invalid Library Item status request", "Configure the provider and supply a known exact Library Item UUID.")
		return
	}
	items, err := d.client.ListLibraryItemStatuses(ctx, data.LibraryItemID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Iru Library Item statuses", err.Error())
		return
	}
	values := make([]libraryItemStatusModel, 0, len(items))
	for _, item := range items {
		value := libraryItemStatusModel{ID: types.Int64Value(item.ID), DeviceID: types.StringValue(item.DeviceID), BlueprintID: types.StringNull(), Status: types.StringValue(item.Status), ReportedAt: types.StringNull()}
		if item.BlueprintID != nil {
			value.BlueprintID = types.StringValue(*item.BlueprintID)
		}
		if item.ReportedAt != nil {
			value.ReportedAt = types.StringValue(*item.ReportedAt)
		}
		values = append(values, value)
	}
	list, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: map[string]attr.Type{"id": types.Int64Type, "device_id": types.StringType, "blueprint_id": types.StringType, "status": types.StringType, "reported_at": types.StringType}}, values)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	data.ID, data.Count, data.Statuses = types.StringValue(strings.ToLower(data.LibraryItemID.ValueString())), types.Int64Value(int64(len(items))), list
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
