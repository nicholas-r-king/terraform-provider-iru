// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
)

type BlueprintRoutingResource struct{ client *client.Client }
type blueprintRoutingModel struct {
	ID               types.String `tfsdk:"id"`
	EnrollmentCode   types.String `tfsdk:"enrollment_code"`
	EnrollmentActive types.Bool   `tfsdk:"enrollment_active"`
}

var (
	_ resource.Resource                   = &BlueprintRoutingResource{}
	_ resource.ResourceWithConfigure      = &BlueprintRoutingResource{}
	_ resource.ResourceWithValidateConfig = &BlueprintRoutingResource{}
	_ resource.ResourceWithImportState    = &BlueprintRoutingResource{}
)

func NewBlueprintRoutingResource() resource.Resource { return &BlueprintRoutingResource{} }
func (*BlueprintRoutingResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_blueprint_routing"
}
func (*BlueprintRoutingResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Manages the existing tenant-wide Blueprint routing enrollment settings. Apply uses PATCH; destroy releases Terraform management without changing Iru because no DELETE is documented. Does not manage routing rules or Assignment Map conditions. Import using tenant.", Attributes: map[string]schema.Attribute{
		"id":                schema.StringAttribute{Computed: true, MarkdownDescription: "Logical singleton identifier: tenant. Iru supplies no remote UUID for these settings.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"enrollment_code":   schema.StringAttribute{Required: true, Sensitive: true, MarkdownDescription: "Manual enrollment code. Stored in Terraform state despite display redaction."},
		"enrollment_active": schema.BoolAttribute{Required: true, MarkdownDescription: "Explicit manual enrollment activation setting. Match the existing setting when importing."},
	}}
}
func (r *BlueprintRoutingResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok || c == nil {
		resp.Diagnostics.AddError("Unexpected Iru client type", "Could not configure the Blueprint routing client.")
		return
	}
	r.client = c
}
func (*BlueprintRoutingResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data blueprintRoutingModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !data.EnrollmentCode.IsNull() && !data.EnrollmentCode.IsUnknown() && !client.ValidEnrollmentCode(data.EnrollmentCode.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("enrollment_code"), "Invalid enrollment code", "enrollment_code must be nonblank and contain no control characters.")
	}
}
func blueprintRoutingState(value client.BlueprintRouting) blueprintRoutingModel {
	return blueprintRoutingModel{ID: types.StringValue("tenant"), EnrollmentCode: types.StringValue(*value.EnrollmentCode.Code), EnrollmentActive: types.BoolValue(*value.EnrollmentCode.Active)}
}
func (data blueprintRoutingModel) resolved() bool {
	return !data.EnrollmentCode.IsNull() && !data.EnrollmentCode.IsUnknown() && !data.EnrollmentActive.IsNull() && !data.EnrollmentActive.IsUnknown()
}
func (r *BlueprintRoutingResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data blueprintRoutingModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil || !data.resolved() {
		resp.Diagnostics.AddError("Blueprint routing client or plan unavailable", "Configure the provider and resolve both enrollment settings before applying.")
		return
	}
	value, err := r.client.GetBlueprintRouting(ctx)
	if err == nil && (*value.EnrollmentCode.Code != data.EnrollmentCode.ValueString() || *value.EnrollmentCode.Active != data.EnrollmentActive.ValueBool()) {
		value, err = r.client.UpdateBlueprintRouting(ctx, data.EnrollmentCode.ValueString(), data.EnrollmentActive.ValueBool())
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to apply Iru Blueprint routing settings", err.Error()+" The setting may have changed; inspect/import it before another apply.")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, blueprintRoutingState(value))...)
}
func (r *BlueprintRoutingResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data blueprintRoutingModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
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
func (r *BlueprintRoutingResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data blueprintRoutingModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil || !data.resolved() {
		resp.Diagnostics.AddError("Blueprint routing client or plan unavailable", "Configure the provider and resolve both enrollment settings before update.")
		return
	}
	value, err := r.client.UpdateBlueprintRouting(ctx, data.EnrollmentCode.ValueString(), data.EnrollmentActive.ValueBool())
	if err != nil {
		resp.Diagnostics.AddError("Unable to update Iru Blueprint routing settings", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, blueprintRoutingState(value))...)
}
func (*BlueprintRoutingResource) Delete(ctx context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.State.RemoveResource(ctx)
	resp.Diagnostics.AddWarning("Blueprint routing management released", "Iru documents no DELETE operation for these tenant settings. The existing enrollment settings remain in Iru.")
}
func (*BlueprintRoutingResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID != "tenant" {
		resp.Diagnostics.AddError("Invalid Blueprint routing import ID", "Import using the literal singleton identifier tenant.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), "tenant")...)
}
