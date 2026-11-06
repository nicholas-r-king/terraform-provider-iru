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

type CustomProfileDataSource struct{ client *client.Client }

type profileLookupModel struct {
	LibraryItemID types.String `tfsdk:"library_item_id"`
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	Profile       types.String `tfsdk:"profile"`
	MDMIdentifier types.String `tfsdk:"mdm_identifier"`
	Active        types.Bool   `tfsdk:"active"`
	RunsOnMac     types.Bool   `tfsdk:"runs_on_mac"`
	RunsOnIphone  types.Bool   `tfsdk:"runs_on_iphone"`
	RunsOnIpad    types.Bool   `tfsdk:"runs_on_ipad"`
	RunsOnTV      types.Bool   `tfsdk:"runs_on_tv"`
	RunsOnVision  types.Bool   `tfsdk:"runs_on_vision"`
}

var (
	_ datasource.DataSource                   = &CustomProfileDataSource{}
	_ datasource.DataSourceWithConfigure      = &CustomProfileDataSource{}
	_ datasource.DataSourceWithValidateConfig = &CustomProfileDataSource{}
)

func NewCustomProfileDataSource() datasource.DataSource { return &CustomProfileDataSource{} }

func (*CustomProfileDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_custom_profile"
}

func (*CustomProfileDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attrs := map[string]schema.Attribute{
		"library_item_id": schema.StringAttribute{Required: true, MarkdownDescription: "Exact existing Custom Profile Library Item UUID. Names and partial IDs are not accepted."},
		"id":              schema.StringAttribute{Computed: true, MarkdownDescription: "Canonical Custom Profile UUID."},
		"name":            schema.StringAttribute{Computed: true, MarkdownDescription: "Current Library Item display name."},
		"profile":         schema.StringAttribute{Computed: true, Sensitive: true, MarkdownDescription: "Exact XML plist returned by Iru, including its managed envelope. Sensitive in displays but stored in Terraform state. Does not reconstruct the original upload or normalize XML."},
		"mdm_identifier":  schema.StringAttribute{Computed: true, MarkdownDescription: "MDM payload identifier reported by Iru."},
		"active":          schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether Iru reports this Library Item as active. Reading never activates it."},
	}
	for name, platform := range map[string]string{"runs_on_mac": "Mac", "runs_on_iphone": "iPhone", "runs_on_ipad": "iPad", "runs_on_tv": "Apple TV", "runs_on_vision": "Apple Vision Pro"} {
		attrs[name] = schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether Iru reports " + platform + " as a target. No false default is invented for missing/null API fields."}
	}
	resp.Schema = schema.Schema{MarkdownDescription: "Reads the configuration fields supported by iru_custom_profile for one exact existing Library Item UUID through GET only. Returns current XML, MDM identity, activation and platform flags; makes no upload, assignment or device changes. Required remote fields that are missing/null or malformed produce an error instead of empty/false values. Not-found and permission failures are errors. The single-object endpoint is not paginated.", Attributes: attrs}
}

func (d *CustomProfileDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok || c == nil {
		resp.Diagnostics.AddError("Unexpected Iru client type", "Could not configure the Custom Profile configuration lookup.")
		return
	}
	d.client = c
}

func (*CustomProfileDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	var id types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("library_item_id"), &id)...)
	if resp.Diagnostics.HasError() || id.IsNull() || id.IsUnknown() {
		return
	}
	if !client.ValidID(id.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("library_item_id"), "Invalid Custom Profile UUID", "Supply the exact existing Library Item UUID, not a name or partial ID.")
	}
}

func (d *CustomProfileDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var id types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("library_item_id"), &id)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if d.client == nil || id.IsNull() || id.IsUnknown() || !client.ValidID(id.ValueString()) {
		resp.Diagnostics.AddError("Custom Profile lookup unavailable", "Configure the provider and supply an exact known Library Item UUID.")
		return
	}
	current, err := d.client.GetProfile(ctx, id.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Iru Custom Profile configuration", err.Error())
		return
	}
	// Report observed bytes, not the managed resource's source-equivalence rules.
	data := profileLookupModel{
		LibraryItemID: id, ID: types.StringValue(current.ID), Name: types.StringValue(current.Name),
		Profile: types.StringValue(current.Profile), MDMIdentifier: types.StringValue(current.MDMIdentifier),
		Active: types.BoolValue(current.Active), RunsOnMac: types.BoolValue(current.RunsOnMac),
		RunsOnIphone: types.BoolValue(current.RunsOnIphone), RunsOnIpad: types.BoolValue(current.RunsOnIpad),
		RunsOnTV: types.BoolValue(current.RunsOnTV), RunsOnVision: types.BoolValue(current.RunsOnVision),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
