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

type CustomAppDataSource struct{ client *client.Client }

type appLookupModel struct {
	LibraryItemID          types.String `tfsdk:"library_item_id"`
	ID                     types.String `tfsdk:"id"`
	Name                   types.String `tfsdk:"name"`
	FileKey                types.String `tfsdk:"file_key"`
	InstallType            types.String `tfsdk:"install_type"`
	InstallEnforcement     types.String `tfsdk:"install_enforcement"`
	AuditScript            types.String `tfsdk:"audit_script"`
	PreinstallScript       types.String `tfsdk:"preinstall_script"`
	PostinstallScript      types.String `tfsdk:"postinstall_script"`
	Active                 types.Bool   `tfsdk:"active"`
	Restart                types.Bool   `tfsdk:"restart"`
	UnzipLocation          types.String `tfsdk:"unzip_location"`
	ShowInSelfService      types.Bool   `tfsdk:"show_in_self_service"`
	SelfServiceCategoryID  types.String `tfsdk:"self_service_category_id"`
	SelfServiceRecommended types.Bool   `tfsdk:"self_service_recommended"`
	SHA256                 types.String `tfsdk:"sha256"`
	FileSize               types.Int64  `tfsdk:"file_size"`
}

var (
	_ datasource.DataSource                   = &CustomAppDataSource{}
	_ datasource.DataSourceWithConfigure      = &CustomAppDataSource{}
	_ datasource.DataSourceWithValidateConfig = &CustomAppDataSource{}
)

func NewCustomAppDataSource() datasource.DataSource { return &CustomAppDataSource{} }

func (*CustomAppDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_custom_app"
}

func (*CustomAppDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Reads one exact Custom App Library Item configuration through GET only. Returns observed installation settings, exact scripts and optional metadata without resource defaults or write constraints. Missing/null core fields fail; optional missing/null values remain null. Does not expose signed URLs/upload credentials or download, upload, assign, activate, install or execute anything. The single-object endpoint is not paginated.", Attributes: map[string]schema.Attribute{
		"library_item_id":          schema.StringAttribute{Required: true, MarkdownDescription: "Exact existing Custom App Library Item UUID. Names and partial IDs are not accepted."},
		"id":                       schema.StringAttribute{Computed: true, MarkdownDescription: "Canonical Custom App UUID."},
		"name":                     schema.StringAttribute{Computed: true, MarkdownDescription: "Current Library Item display name."},
		"file_key":                 schema.StringAttribute{Computed: true, Sensitive: true, MarkdownDescription: "Reported uploaded installer key. Sensitive in displays because keys can reveal private paths/names; stored in state. Not an installer export or download URL."},
		"install_type":             schema.StringAttribute{Computed: true, MarkdownDescription: "Observed package, zip or image installation type."},
		"install_enforcement":      schema.StringAttribute{Computed: true, MarkdownDescription: "Observed install_once, continuously_enforce or no_enforcement setting. Reading never enforces installation."},
		"audit_script":             schema.StringAttribute{Computed: true, Sensitive: true, MarkdownDescription: "Exact audit-script bytes returned by Iru. Sensitive in displays and stored in state."},
		"preinstall_script":        schema.StringAttribute{Computed: true, Sensitive: true, MarkdownDescription: "Exact preinstall-script bytes returned by Iru. Sensitive in displays and stored in state."},
		"postinstall_script":       schema.StringAttribute{Computed: true, Sensitive: true, MarkdownDescription: "Exact postinstall-script bytes returned by Iru. Sensitive in displays and stored in state."},
		"active":                   schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether Iru reports this Library Item as active. Reading never activates it."},
		"restart":                  schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether Iru reports restart enabled. Reading never restarts a device."},
		"unzip_location":           schema.StringAttribute{Computed: true, MarkdownDescription: "Observed extraction location, or null when omitted/null. Empty remains empty."},
		"show_in_self_service":     schema.BoolAttribute{Computed: true, MarkdownDescription: "Observed Self Service visibility, or null when omitted/null, distinct from false."},
		"self_service_category_id": schema.StringAttribute{Computed: true, MarkdownDescription: "Reported Self Service category UUID, or null when omitted/null. Preserved while hidden."},
		"self_service_recommended": schema.BoolAttribute{Computed: true, MarkdownDescription: "Reported recommendation flag, or null when omitted/null, distinct from false. Preserved while hidden."},
		"sha256":                   schema.StringAttribute{Computed: true, MarkdownDescription: "Reported installer SHA256, or null when omitted/null. Never computed from the key or treated as verification of downloaded bytes."},
		"file_size":                schema.Int64Attribute{Computed: true, MarkdownDescription: "Reported installer size, or null when omitted/null. Zero remains known zero; no binary is downloaded."},
	}}
}

func (d *CustomAppDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok || c == nil {
		resp.Diagnostics.AddError("Unexpected Iru client type", "Could not configure the Custom App configuration lookup.")
		return
	}
	d.client = c
}

func (*CustomAppDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	var id types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("library_item_id"), &id)...)
	if resp.Diagnostics.HasError() || id.IsNull() || id.IsUnknown() {
		return
	}
	if !client.ValidID(id.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("library_item_id"), "Invalid Custom App UUID", "Supply the exact existing Library Item UUID, not a name or partial ID.")
	}
}

func (d *CustomAppDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var id types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("library_item_id"), &id)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if d.client == nil || id.IsNull() || id.IsUnknown() || !client.ValidID(id.ValueString()) {
		resp.Diagnostics.AddError("Custom App lookup unavailable", "Configure the provider and supply an exact known Library Item UUID.")
		return
	}
	current, err := d.client.GetAppConfiguration(ctx, id.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Iru Custom App configuration", err.Error())
		return
	}
	// Observe configuration without source equivalence, defaults or upload metadata.
	data := appLookupModel{LibraryItemID: id,
		ID:                     types.StringValue(current.ID),
		Name:                   types.StringValue(current.Name),
		FileKey:                types.StringValue(current.FileKey),
		InstallType:            types.StringValue(current.InstallType),
		InstallEnforcement:     types.StringValue(current.InstallEnforcement),
		AuditScript:            types.StringValue(current.AuditScript),
		PreinstallScript:       types.StringValue(current.PreinstallScript),
		PostinstallScript:      types.StringValue(current.PostinstallScript),
		Active:                 types.BoolValue(current.Active),
		Restart:                types.BoolValue(current.Restart),
		UnzipLocation:          types.StringPointerValue(current.UnzipLocation),
		ShowInSelfService:      types.BoolPointerValue(current.ShowInSelfService),
		SelfServiceCategoryID:  types.StringPointerValue(current.SelfServiceCategoryID),
		SelfServiceRecommended: types.BoolPointerValue(current.SelfServiceRecommended),
		SHA256:                 types.StringPointerValue(current.SHA256),
		FileSize:               types.Int64PointerValue(current.FileSize),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
