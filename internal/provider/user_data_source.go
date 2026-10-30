// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
)

type UserDataSource struct{ client *client.Client }
type userModel struct {
	UserID   types.String `tfsdk:"user_id"`
	ID       types.String `tfsdk:"id"`
	Name     types.String `tfsdk:"name"`
	Email    types.String `tfsdk:"email"`
	Active   types.Bool   `tfsdk:"active"`
	Archived types.Bool   `tfsdk:"archived"`
}

func NewUserDataSource() datasource.DataSource { return &UserDataSource{} }
func (*UserDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}
func (*UserDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Reads one existing directory integration user by exact UUID. Returns identity/status metadata for selecting device assignments. Personal metadata remains in Terraform state. Does not create, modify or delete the directory user.", Attributes: map[string]schema.Attribute{
		"user_id":  schema.StringAttribute{Required: true, MarkdownDescription: "Exact directory-user UUID. Deprecated integer IDs and name/email searches are not accepted."},
		"id":       schema.StringAttribute{Computed: true, MarkdownDescription: "Canonical directory-user UUID."},
		"name":     schema.StringAttribute{Computed: true, MarkdownDescription: "Reported directory display name, or null if omitted."},
		"email":    schema.StringAttribute{Computed: true, MarkdownDescription: "Reported directory email address, or null if omitted."},
		"active":   schema.BoolAttribute{Computed: true, MarkdownDescription: "Reported active status, or null if omitted."},
		"archived": schema.BoolAttribute{Computed: true, MarkdownDescription: "Reported archived status, or null if omitted."},
	}}
}
func (r *UserDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok || c == nil {
		resp.Diagnostics.AddError("Unexpected Iru client type", "Could not configure the directory user lookup.")
		return
	}
	r.client = c
}
func (r *UserDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data userModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil || data.UserID.IsNull() || data.UserID.IsUnknown() || !client.ValidID(data.UserID.ValueString()) {
		resp.Diagnostics.AddError("Invalid directory user lookup", "Configure the provider and provide a known exact user UUID.")
		return
	}
	user, err := r.client.GetUser(ctx, data.UserID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Iru directory user", err.Error())
		return
	}
	data.ID = types.StringValue(user.ID)
	data.Name, data.Email = types.StringNull(), types.StringNull()
	data.Active, data.Archived = types.BoolNull(), types.BoolNull()
	if user.Name != nil {
		data.Name = types.StringValue(*user.Name)
	}
	if user.Email != nil {
		data.Email = types.StringValue(*user.Email)
	}
	if user.Active != nil {
		data.Active = types.BoolValue(*user.Active)
	}
	if user.Archived != nil {
		data.Archived = types.BoolValue(*user.Archived)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
