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

type InHouseAppDataSource struct{ client *client.Client }

type inHouseLookupModel struct {
	LibraryItemID types.String `tfsdk:"library_item_id"`
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	FileKey       types.String `tfsdk:"file_key"`
	Active        types.Bool   `tfsdk:"active"`
	RunsOnIphone  types.Bool   `tfsdk:"runs_on_iphone"`
	RunsOnIpad    types.Bool   `tfsdk:"runs_on_ipad"`
	RunsOnTV      types.Bool   `tfsdk:"runs_on_tv"`
	SHA256        types.String `tfsdk:"sha256"`
	FileSize      types.Int64  `tfsdk:"file_size"`
}

var (
	_ datasource.DataSource                   = &InHouseAppDataSource{}
	_ datasource.DataSourceWithConfigure      = &InHouseAppDataSource{}
	_ datasource.DataSourceWithValidateConfig = &InHouseAppDataSource{}
)

func NewInHouseAppDataSource() datasource.DataSource { return &InHouseAppDataSource{} }

func (*InHouseAppDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_in_house_app"
}

func (*InHouseAppDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attrs := map[string]schema.Attribute{
		"library_item_id": schema.StringAttribute{Required: true, MarkdownDescription: "Exact existing In-House App Library Item UUID. Names and partial IDs are not accepted."},
		"id":              schema.StringAttribute{Computed: true, MarkdownDescription: "Canonical In-House App UUID."},
		"name":            schema.StringAttribute{Computed: true, MarkdownDescription: "Current Library Item display name."},
		"file_key":        schema.StringAttribute{Computed: true, Sensitive: true, MarkdownDescription: "Reported IPA installer key. Sensitive in displays because it may reveal private names/paths, but stored in Terraform state. Not a download URL or installer export."},
		"active":          schema.BoolAttribute{Computed: true, MarkdownDescription: "Observed activation flag. Reading never activates the item."},
		"sha256":          schema.StringAttribute{Computed: true, MarkdownDescription: "Reported IPA SHA256, or null when missing/null. Letter case is preserved. No installer bytes are downloaded or verified."},
		"file_size":       schema.Int64Attribute{Computed: true, MarkdownDescription: "Reported IPA byte size, or null when missing/null. Zero remains known zero."},
	}
	for name, platform := range map[string]string{"runs_on_iphone": "iPhone", "runs_on_ipad": "iPad", "runs_on_tv": "Apple TV"} {
		attrs[name] = schema.BoolAttribute{Computed: true, MarkdownDescription: "Observed " + platform + " target flag. Missing/null is an error, not an invented false default."}
	}
	resp.Schema = schema.Schema{MarkdownDescription: "Reads the supported In-House App resource configuration and optional IPA hash/size for one exact existing Library Item UUID through GET only. Observes flags without managed-resource platform defaults or write constraints. Missing/null required fields fail; missing/null optional metadata stays null. Excludes signed URLs, upload credentials, installer bytes and parsed IPA metadata. Never uploads, assigns, activates, installs or deletes anything. The single-object endpoint is not paginated.", Attributes: attrs}
}

func (d *InHouseAppDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok || c == nil {
		resp.Diagnostics.AddError("Unexpected Iru client type", "Could not configure the In-House App configuration lookup.")
		return
	}
	d.client = c
}

func (*InHouseAppDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	var id types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("library_item_id"), &id)...)
	if resp.Diagnostics.HasError() || id.IsNull() || id.IsUnknown() {
		return
	}
	if !client.ValidID(id.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("library_item_id"), "Invalid In-House App UUID", "Supply the exact existing Library Item UUID, not a name or partial ID.")
	}
}

func (d *InHouseAppDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var id types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("library_item_id"), &id)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if d.client == nil || id.IsNull() || id.IsUnknown() || !client.ValidID(id.ValueString()) {
		resp.Diagnostics.AddError("In-House App lookup unavailable", "Configure the provider and supply an exact known Library Item UUID.")
		return
	}
	current, err := d.client.GetInHouseAppConfiguration(ctx, id.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Iru In-House App configuration", err.Error())
		return
	}
	data := inHouseLookupModel{
		LibraryItemID: id, ID: types.StringValue(current.ID), Name: types.StringValue(current.Name),
		FileKey: types.StringValue(current.FileKey), Active: types.BoolValue(current.Active),
		RunsOnIphone: types.BoolValue(current.RunsOnIphone), RunsOnIpad: types.BoolValue(current.RunsOnIpad), RunsOnTV: types.BoolValue(current.RunsOnTV),
		SHA256: types.StringPointerValue(current.SHA256), FileSize: types.Int64PointerValue(current.FileSize),
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
