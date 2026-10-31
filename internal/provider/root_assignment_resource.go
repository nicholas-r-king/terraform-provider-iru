// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
)

type RootAssignmentResource struct{ client *client.Client }
type rootAssignmentModel struct {
	ID            types.String `tfsdk:"id"`
	BlueprintID   types.String `tfsdk:"blueprint_id"`
	LibraryItemID types.String `tfsdk:"library_item_id"`
}

var (
	_ resource.Resource                   = &RootAssignmentResource{}
	_ resource.ResourceWithConfigure      = &RootAssignmentResource{}
	_ resource.ResourceWithValidateConfig = &RootAssignmentResource{}
	_ resource.ResourceWithImportState    = &RootAssignmentResource{}
)

func NewRootAssignmentResource() resource.Resource { return &RootAssignmentResource{} }
func (*RootAssignmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_blueprint_library_item_assignment"
}
func assignmentReplaceModifier() planmodifier.String {
	return stringplanmodifier.RequiresReplaceIf(func(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
		resp.RequiresReplace = req.PlanValue.IsUnknown() || req.StateValue.IsUnknown() || !strings.EqualFold(req.PlanValue.ValueString(), req.StateValue.ValueString())
	}, "Changing the UUID replaces only this membership; UUID casing alone does not mutate Iru.", "Changing the UUID replaces only this membership; UUID casing alone does not mutate Iru.")
}
func (*RootAssignmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Manages one Library Item membership in a classic Blueprint root. Does not create/delete the Blueprint or Library Item. Assignment can apply policies to that Blueprint's devices; detaching can remove policy. Assignment Maps and conditional node placement are not supported because aggregate membership does not prove node placement. Import by blueprint_uuid/library_item_uuid.", Attributes: map[string]schema.Attribute{
		"id":              schema.StringAttribute{Computed: true, MarkdownDescription: "Canonical blueprint_uuid/library_item_uuid composite identity.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"blueprint_id":    schema.StringAttribute{Required: true, MarkdownDescription: "Exact existing classic Blueprint UUID. Changing it replaces this membership.", PlanModifiers: []planmodifier.String{assignmentReplaceModifier()}},
		"library_item_id": schema.StringAttribute{Required: true, MarkdownDescription: "Exact existing Library Item UUID. Changing it detaches the old pair and assigns the new pair.", PlanModifiers: []planmodifier.String{assignmentReplaceModifier()}},
	}}
}
func (r *RootAssignmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok || c == nil {
		resp.Diagnostics.AddError("Unexpected Iru client type", "Could not configure root assignment.")
		return
	}
	r.client = c
}
func (v rootAssignmentModel) canonicalID() string {
	return strings.ToLower(v.BlueprintID.ValueString() + "/" + v.LibraryItemID.ValueString())
}
func (v rootAssignmentModel) valid(requireID bool) bool {
	if v.BlueprintID.IsNull() || v.BlueprintID.IsUnknown() || v.LibraryItemID.IsNull() || v.LibraryItemID.IsUnknown() || !client.ValidID(v.BlueprintID.ValueString()) || !client.ValidID(v.LibraryItemID.ValueString()) {
		return false
	}
	return !requireID || (!v.ID.IsNull() && !v.ID.IsUnknown() && v.ID.ValueString() == v.canonicalID())
}
func (*RootAssignmentResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var v rootAssignmentModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &v)...)
	if resp.Diagnostics.HasError() {
		return
	}
	for name, value := range map[string]types.String{"blueprint_id": v.BlueprintID, "library_item_id": v.LibraryItemID} {
		if !value.IsUnknown() && !value.IsNull() && !client.ValidID(value.ValueString()) {
			resp.Diagnostics.AddAttributeError(path.Root(name), "Invalid assignment UUID", "Supply an exact existing UUID, not a name or node identifier.")
		}
	}
}
func (r *RootAssignmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var v rootAssignmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &v)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil || !v.valid(false) {
		resp.Diagnostics.AddError("Root assignment client or plan unavailable", "Configure the provider and resolve both UUIDs before assignment.")
		return
	}
	v.ID = types.StringValue(v.canonicalID())
	attempted, err := r.client.SetClassicRootAssignment(ctx, v.BlueprintID.ValueString(), v.LibraryItemID.ValueString(), true)
	if err != nil {
		if attempted {
			resp.Diagnostics.Append(resp.State.Set(ctx, &v)...)
		}
		resp.Diagnostics.AddError("Unable to assign Library Item", err.Error()+" Membership may have changed; inspect/import the exact pair before another apply.")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &v)...)
}
func (r *RootAssignmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var v rootAssignmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &v)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil || !v.valid(true) {
		resp.Diagnostics.AddError("Invalid root assignment state", "Configure the provider and retain the exact composite identity and UUID pair.")
		return
	}
	present, err := r.client.ClassicRootAssignment(ctx, v.BlueprintID.ValueString(), v.LibraryItemID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read root assignment", err.Error())
		return
	}
	if !present {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &v)...)
}
func (*RootAssignmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var prior, next rootAssignmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &next)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !prior.valid(true) || !next.valid(false) || prior.canonicalID() != next.canonicalID() {
		resp.Diagnostics.AddError("Root assignment replacement required", "Changing either UUID requires replacing only the membership. No in-place reassignment was attempted.")
		return
	}
	next.ID = types.StringValue(next.canonicalID())
	resp.Diagnostics.Append(resp.State.Set(ctx, &next)...)
}
func (r *RootAssignmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var v rootAssignmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &v)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil || !v.valid(true) {
		resp.Diagnostics.AddError("Invalid root assignment state", "Configure the provider and retain the exact captured UUID pair before detaching.")
		return
	}
	if _, err := r.client.SetClassicRootAssignment(ctx, v.BlueprintID.ValueString(), v.LibraryItemID.ValueString(), false); err != nil {
		resp.Diagnostics.AddError("Unable to detach Library Item", err.Error()+" Retain state and reconcile the exact pair before retrying.")
	}
}
func (*RootAssignmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 2 || !client.ValidID(parts[0]) || !client.ValidID(parts[1]) {
		resp.Diagnostics.AddError("Invalid root assignment import ID", "Use blueprint_uuid/library_item_uuid for an existing classic root membership.")
		return
	}
	v := rootAssignmentModel{ID: types.StringValue(strings.ToLower(req.ID)), BlueprintID: types.StringValue(strings.ToLower(parts[0])), LibraryItemID: types.StringValue(strings.ToLower(parts[1]))}
	resp.Diagnostics.Append(resp.State.Set(ctx, &v)...)
}
