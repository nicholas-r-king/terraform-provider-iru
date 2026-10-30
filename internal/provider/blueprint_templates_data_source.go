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

type BlueprintTemplatesDataSource struct{ client *client.Client }
type blueprintTemplatesModel struct {
	ID        types.String `tfsdk:"id"`
	Templates types.List   `tfsdk:"templates"`
}
type blueprintTemplateModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	CategoryID   types.String `tfsdk:"category_id"`
	CategoryName types.String `tfsdk:"category_name"`
}

func NewBlueprintTemplatesDataSource() datasource.DataSource { return &BlueprintTemplatesDataSource{} }
func (*BlueprintTemplatesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_blueprint_templates"
}
func (*BlueprintTemplatesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Lists built-in Blueprint template identities and category names for selecting a create/clone source. Template IDs are positive integers represented as strings, suitable for iru_blueprint.source_id with source_type=template. Excludes parameter definitions and descriptions. Results are complete, bounded and sorted by numeric template ID.", Attributes: map[string]schema.Attribute{
		"id": schema.StringAttribute{Computed: true, MarkdownDescription: "Logical catalog identifier: blueprint_templates."},
		"templates": schema.ListNestedAttribute{Computed: true, MarkdownDescription: "Template/catalog identity projection.", NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
			"id":            schema.StringAttribute{Computed: true, MarkdownDescription: "Positive integer template ID represented as a string."},
			"name":          schema.StringAttribute{Computed: true, MarkdownDescription: "Template name."},
			"category_id":   schema.StringAttribute{Computed: true, MarkdownDescription: "Positive integer category ID represented as a string."},
			"category_name": schema.StringAttribute{Computed: true, MarkdownDescription: "Category name."},
		}}},
	}}
}
func (r *BlueprintTemplatesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok || c == nil {
		resp.Diagnostics.AddError("Unexpected Iru client type", "Could not configure the Blueprint template catalog lookup.")
		return
	}
	r.client = c
}
func (r *BlueprintTemplatesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Iru client unavailable", "Configure the provider before reading Blueprint templates.")
		return
	}
	items, err := r.client.ListBlueprintTemplates(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Iru Blueprint templates", err.Error())
		return
	}
	values := make([]blueprintTemplateModel, 0, len(items))
	for _, item := range items {
		values = append(values, blueprintTemplateModel{ID: types.StringValue(item.ID), Name: types.StringValue(item.Name), CategoryID: types.StringValue(item.CategoryID), CategoryName: types.StringValue(item.CategoryName)})
	}
	list, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: map[string]attr.Type{"id": types.StringType, "name": types.StringType, "category_id": types.StringType, "category_name": types.StringType}}, values)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, blueprintTemplatesModel{ID: types.StringValue("blueprint_templates"), Templates: list})...)
}
