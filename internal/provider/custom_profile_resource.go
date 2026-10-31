// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
)

var (
	_ resource.Resource                   = &CustomProfileResource{}
	_ resource.ResourceWithConfigure      = &CustomProfileResource{}
	_ resource.ResourceWithImportState    = &CustomProfileResource{}
	_ resource.ResourceWithValidateConfig = &CustomProfileResource{}
)

type CustomProfileResource struct{ client *client.Client }

type profileModel struct {
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	Profile       types.String `tfsdk:"profile"`
	Active        types.Bool   `tfsdk:"active"`
	RunsOnMac     types.Bool   `tfsdk:"runs_on_mac"`
	RunsOnIphone  types.Bool   `tfsdk:"runs_on_iphone"`
	RunsOnIpad    types.Bool   `tfsdk:"runs_on_ipad"`
	RunsOnTV      types.Bool   `tfsdk:"runs_on_tv"`
	RunsOnVision  types.Bool   `tfsdk:"runs_on_vision"`
	MDMIdentifier types.String `tfsdk:"mdm_identifier"`
}

func NewCustomProfileResource() resource.Resource { return &CustomProfileResource{} }

func (*CustomProfileResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_custom_profile"
}

func (*CustomProfileResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	attributes := map[string]schema.Attribute{
		"id":             schema.StringAttribute{Computed: true, MarkdownDescription: "Library Item UUID.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"name":           schema.StringAttribute{Required: true, MarkdownDescription: "Display name."},
		"profile":        schema.StringAttribute{Required: true, Sensitive: true, MarkdownDescription: "Unsigned XML plist content sent as a mobileconfig file. Stored verbatim in Terraform state; do not embed secrets."},
		"mdm_identifier": schema.StringAttribute{Computed: true, MarkdownDescription: "MDM identifier returned by Iru."},
		"active":         schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), MarkdownDescription: "Whether the Library Item is active. Defaults to false."},
	}
	for name, platform := range map[string]string{"runs_on_mac": "macOS", "runs_on_iphone": "iPhone", "runs_on_ipad": "iPad", "runs_on_tv": "Apple TV", "runs_on_vision": "Apple Vision"} {
		attributes[name] = schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), MarkdownDescription: "Target " + platform + ". Defaults to false; choose at least one supported platform."}
	}
	resp.Schema = schema.Schema{MarkdownDescription: "Manages one Iru Custom Profile Library Item with explicit target platforms. Does not manage assignments or device inventory. Import by Library Item UUID.", Attributes: attributes}
}

func (r *CustomProfileResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok || c == nil {
		resp.Diagnostics.AddError("Unexpected Iru client type", "The provider could not configure its Custom Profiles client.")
		return
	}
	r.client = c
}

func (data profileModel) input() client.ProfileInput {
	return client.ProfileInput{Name: data.Name.ValueString(), Profile: data.Profile.ValueString(), Active: data.Active.ValueBool(), RunsOnMac: data.RunsOnMac.ValueBool(),
		RunsOnIphone: data.RunsOnIphone.ValueBool(), RunsOnIpad: data.RunsOnIpad.ValueBool(), RunsOnTV: data.RunsOnTV.ValueBool(), RunsOnVision: data.RunsOnVision.ValueBool()}
}

func (data profileModel) unknownInput() bool {
	return data.Name.IsUnknown() || data.Profile.IsUnknown() || data.Active.IsUnknown() || data.RunsOnMac.IsUnknown() || data.RunsOnIphone.IsUnknown() || data.RunsOnIpad.IsUnknown() || data.RunsOnTV.IsUnknown() || data.RunsOnVision.IsUnknown()
}

func (*CustomProfileResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data profileModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || data.unknownInput() {
		return
	}
	if err := data.input().Validate(); err != nil {
		resp.Diagnostics.AddError("Invalid Custom Profile configuration", err.Error())
	}
}

func profileState(profile client.Profile, prior ...profileModel) profileModel {
	data := profileModel{ID: types.StringValue(profile.ID), Name: types.StringValue(profile.Name), Profile: types.StringValue(profile.Profile), MDMIdentifier: types.StringValue(profile.MDMIdentifier),
		Active: types.BoolValue(profile.Active), RunsOnMac: types.BoolValue(profile.RunsOnMac), RunsOnIphone: types.BoolValue(profile.RunsOnIphone), RunsOnIpad: types.BoolValue(profile.RunsOnIpad), RunsOnTV: types.BoolValue(profile.RunsOnTV), RunsOnVision: types.BoolValue(profile.RunsOnVision)}
	if len(prior) == 1 && !prior[0].Profile.IsNull() && !prior[0].Profile.IsUnknown() && client.ProfileSourceEquivalent(prior[0].Profile.ValueString(), profile) {
		data.Profile = prior[0].Profile
	}
	return data
}

func (r *CustomProfileResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data profileModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil || data.unknownInput() {
		resp.Diagnostics.AddError("Custom Profile client or plan unavailable", "Configure the provider and resolve all profile inputs before creation.")
		return
	}
	created, err := r.client.CreateProfile(ctx, data.input())
	if err != nil {
		if client.ValidID(created.ID) {
			data.ID, data.MDMIdentifier = types.StringValue(strings.ToLower(created.ID)), types.StringNull()
			resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
		}
		resp.Diagnostics.AddError("Unable to create Iru Custom Profile", err.Error()+" Inspect/import any created item before retrying. A failed creation may be tainted.")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, profileState(created, data))...)
}

func (r *CustomProfileResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data profileModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Iru client unavailable", "Configure the provider before reading a Custom Profile.")
		return
	}
	profile, err := r.client.GetProfile(ctx, data.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Iru Custom Profile", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, profileState(profile, data))...)
}

func (r *CustomProfileResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data, prior profileModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil || data.unknownInput() {
		resp.Diagnostics.AddError("Custom Profile client or plan unavailable", "Configure the provider and resolve all profile inputs before update.")
		return
	}
	profile, err := r.client.UpdateProfile(ctx, data.ID.ValueString(), data.input(), data.Profile.ValueString() != prior.Profile.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to update Iru Custom Profile", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, profileState(profile, data))...)
}

func (r *CustomProfileResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data profileModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Iru client unavailable", "Configure the provider before deleting a Custom Profile.")
		return
	}
	if err := r.client.DeleteProfile(ctx, data.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Unable to delete Iru Custom Profile", err.Error())
	}
}

func (*CustomProfileResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if !client.ValidID(req.ID) {
		resp.Diagnostics.AddError("Invalid Custom Profile import ID", "Import using the Library Item UUID.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), strings.ToLower(req.ID))...)
}
