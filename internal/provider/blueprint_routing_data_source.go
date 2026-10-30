// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
)

type BlueprintRoutingDataSource struct{ client *client.Client }

func NewBlueprintRoutingDataSource() datasource.DataSource { return &BlueprintRoutingDataSource{} }
func (*BlueprintRoutingDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_blueprint_routing"
}
func (*BlueprintRoutingDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Reads tenant-wide Blueprint routing manual enrollment settings. The code is sensitive and remains in Terraform state. Does not read routing rules.", Attributes: map[string]schema.Attribute{
		"id":                schema.StringAttribute{Computed: true, MarkdownDescription: "Logical singleton identifier: tenant."},
		"enrollment_code":   schema.StringAttribute{Computed: true, Sensitive: true, MarkdownDescription: "Current manual enrollment code. Stored in Terraform state."},
		"enrollment_active": schema.BoolAttribute{Computed: true, MarkdownDescription: "Current manual enrollment activation setting."},
	}}
}
func (r *BlueprintRoutingDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok || c == nil {
		resp.Diagnostics.AddError("Unexpected Iru client type", "Could not configure the Blueprint routing lookup.")
		return
	}
	r.client = c
}
func (r *BlueprintRoutingDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Iru client unavailable", "Configure the provider before reading Blueprint routing settings.")
		return
	}
	value, err := r.client.GetBlueprintRouting(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Iru Blueprint routing settings", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, blueprintRoutingState(value))...)
}
