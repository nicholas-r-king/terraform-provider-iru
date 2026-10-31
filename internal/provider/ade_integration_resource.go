// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
)

type ADEIntegrationResource struct{ client *client.Client }
type adeModel struct {
	ID                  types.String `tfsdk:"id"`
	BlueprintID         types.String `tfsdk:"blueprint_id"`
	UseBlueprintRouting types.Bool   `tfsdk:"use_blueprint_routing"`
	Email               types.String `tfsdk:"email"`
	Phone               types.String `tfsdk:"phone"`
	ServerToken         types.String `tfsdk:"server_token"`
	ServerTokenVersion  types.Int64  `tfsdk:"server_token_version"`
	ServerName          types.String `tfsdk:"server_name"`
	AccessTokenExpiry   types.String `tfsdk:"access_token_expiry"`
}

var (
	_ resource.Resource                   = &ADEIntegrationResource{}
	_ resource.ResourceWithConfigure      = &ADEIntegrationResource{}
	_ resource.ResourceWithImportState    = &ADEIntegrationResource{}
	_ resource.ResourceWithValidateConfig = &ADEIntegrationResource{}
	_ resource.ResourceWithModifyPlan     = &ADEIntegrationResource{}
)

func NewADEIntegrationResource() resource.Resource { return &ADEIntegrationResource{} }
func (*ADEIntegrationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ade_integration"
}
func (*ADEIntegrationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Manages an Apple Automated Device Enrollment integration and renewal of its Apple server token. Import by integration UUID. Creation and renewal require an Apple Business Manager token. Deletion is blocked because the Iru endpoint unassigns associated devices and the provider cannot verify a safe conditional deletion. Retain the remote integration when removing Terraform management. Requires Terraform 1.11 or later for write-only token input.", Attributes: map[string]schema.Attribute{
		"id":                    schema.StringAttribute{Computed: true, MarkdownDescription: "ADE integration UUID.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"blueprint_id":          schema.StringAttribute{Optional: true, MarkdownDescription: "Default Blueprint UUID. Required unless use_blueprint_routing=true; omit when using routing."},
		"use_blueprint_routing": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), MarkdownDescription: "Use Blueprint Routing for enrollment. Defaults to false; true requires blueprint_id to be omitted. Set both assignment choices to match the integration before importing."},
		"email":                 schema.StringAttribute{Required: true, MarkdownDescription: "Default enrollment contact email."},
		"phone":                 schema.StringAttribute{Required: true, MarkdownDescription: "Default enrollment contact phone."},
		"server_token":          schema.StringAttribute{Optional: true, WriteOnly: true, Sensitive: true, MarkdownDescription: "Base64-encoded Apple .p7m token; use filebase64 or an ephemeral value. Required at create and when server_token_version changes. Never stored in plan or state."},
		"server_token_version":  schema.Int64Attribute{Optional: true, Computed: true, Default: int64default.StaticInt64(0), MarkdownDescription: "Nonnegative local renewal trigger. Change this when supplying a new server_token; unchanged versions do not resend token bytes. Import defaults to 0. The version is local bookkeeping, not remotely detectable token drift."},
		"server_name":           schema.StringAttribute{Computed: true, MarkdownDescription: "Apple server name returned by Iru."},
		"access_token_expiry":   schema.StringAttribute{Computed: true, MarkdownDescription: "Token expiration timestamp returned by Iru."},
	}}
}
func (r *ADEIntegrationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok || c == nil {
		resp.Diagnostics.AddError("Unexpected Iru client type", "Could not configure the ADE integration client.")
		return
	}
	r.client = c
}
func (data adeModel) input() client.ADEInput {
	return client.ADEInput{BlueprintID: data.BlueprintID.ValueString(), UseBlueprintRouting: data.UseBlueprintRouting.ValueBool(), Email: data.Email.ValueString(), Phone: data.Phone.ValueString()}
}
func decodeServerToken(value types.String) ([]byte, error) {
	if value.IsNull() || value.IsUnknown() || len(value.ValueString()) > base64.StdEncoding.EncodedLen(1024*1024) {
		return nil, errors.New("supply a known base64 Apple server_token of at most 1 MiB for create/renew")
	}
	bytes, err := base64.StdEncoding.DecodeString(value.ValueString())
	if err != nil || len(bytes) == 0 || len(bytes) > 1024*1024 {
		return nil, errors.New("server_token must encode a nonempty Apple .p7m token of at most 1 MiB (content withheld)")
	}
	return bytes, nil
}
func (*ADEIntegrationResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data adeModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !data.ServerTokenVersion.IsUnknown() && !data.ServerTokenVersion.IsNull() && data.ServerTokenVersion.ValueInt64() < 0 {
		resp.Diagnostics.AddAttributeError(path.Root("server_token_version"), "Invalid token version", "server_token_version must be nonnegative.")
	}
	if !data.ServerToken.IsNull() && !data.ServerToken.IsUnknown() {
		if _, err := decodeServerToken(data.ServerToken); err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("server_token"), "Invalid Apple server token", err.Error())
		}
	}
	if data.BlueprintID.IsUnknown() || data.UseBlueprintRouting.IsUnknown() || data.Email.IsUnknown() || data.Phone.IsUnknown() {
		return
	}
	if data.UseBlueprintRouting.ValueBool() && !data.BlueprintID.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("blueprint_id"), "Conflicting ADE enrollment assignment", "Omit blueprint_id when use_blueprint_routing is true; an empty configured string is not an omitted Blueprint.")
		return
	}
	if err := data.input().Validate(); err != nil {
		resp.Diagnostics.AddError("Invalid ADE configuration", err.Error())
	}
}
func adeState(value client.ADEIntegration, prior adeModel) adeModel {
	prior.ID = types.StringValue(value.ID)
	if value.BlueprintID == nil {
		prior.BlueprintID = types.StringNull()
	} else if !strings.EqualFold(prior.BlueprintID.ValueString(), *value.BlueprintID) {
		prior.BlueprintID = types.StringValue(*value.BlueprintID)
	}
	prior.UseBlueprintRouting = types.BoolValue(value.UseBlueprintRouting)
	prior.Email, prior.Phone = types.StringValue(*value.Defaults.Email), types.StringValue(*value.Defaults.Phone)
	prior.ServerName, prior.AccessTokenExpiry, prior.ServerToken = types.StringValue(value.ServerName), types.StringValue(value.AccessTokenExpiry), types.StringNull()
	if prior.ServerTokenVersion.IsNull() {
		prior.ServerTokenVersion = types.Int64Value(0)
	}
	return prior
}
func (data adeModel) unknownInput() bool {
	return data.BlueprintID.IsUnknown() || data.UseBlueprintRouting.IsUnknown() || data.Email.IsUnknown() || data.Phone.IsUnknown() || data.ServerTokenVersion.IsUnknown()
}
func (r *ADEIntegrationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data adeModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil || data.unknownInput() {
		resp.Diagnostics.AddError("ADE client or plan unavailable", "Configure the provider and resolve ADE metadata before creation.")
		return
	}
	var configured types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("server_token"), &configured)...)
	if resp.Diagnostics.HasError() {
		return
	}
	token, err := decodeServerToken(configured)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("server_token"), "Apple server token required", err.Error())
		return
	}
	value, err := r.client.CreateADEIntegration(ctx, data.input(), token)
	if err != nil {
		if client.ValidID(value.ID) {
			data.ID, data.ServerName, data.AccessTokenExpiry, data.ServerToken = types.StringValue(strings.ToLower(value.ID)), types.StringNull(), types.StringNull(), types.StringNull()
			resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
		}
		resp.Diagnostics.AddError("Unable to create Iru ADE integration", err.Error()+" Inspect/import a potentially created integration before retrying.")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, adeState(value, data))...)
}
func (r *ADEIntegrationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data adeModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Iru client unavailable", "Configure the provider before reading an ADE integration.")
		return
	}
	value, err := r.client.GetADEIntegration(ctx, data.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Iru ADE integration", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, adeState(value, data))...)
}
func (r *ADEIntegrationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data, prior adeModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil || data.unknownInput() {
		resp.Diagnostics.AddError("ADE client or plan unavailable", "Configure the provider and resolve ADE metadata before update.")
		return
	}
	var value client.ADEIntegration
	var err error
	if !data.ServerTokenVersion.Equal(prior.ServerTokenVersion) {
		var configured types.String
		resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("server_token"), &configured)...)
		if resp.Diagnostics.HasError() {
			return
		}
		var token []byte
		token, err = decodeServerToken(configured)
		if err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("server_token"), "Apple server token required for renewal", err.Error())
			return
		}
		value, err = r.client.RenewADEIntegration(ctx, data.ID.ValueString(), data.input(), token)
	} else {
		value, err = r.client.UpdateADEIntegration(ctx, data.ID.ValueString(), data.input())
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to update Iru ADE integration", err.Error()+" Reconcile an ambiguous renewal before retrying.")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, adeState(value, data))...)
}

const adeDeletionBlocked = "Iru ADE integration deletion unassigns associated devices. A safe conditional deletion contract has not been verified, so the provider refuses DELETE and retains Terraform state. Review device associations outside Terraform. To retain the integration and stop managing it, use a removed block with lifecycle { destroy = false }."

func (*ADEIntegrationResource) ModifyPlan(_ context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if !req.State.Raw.IsNull() && req.Plan.Raw.IsNull() {
		resp.Diagnostics.AddError("ADE integration deletion blocked", adeDeletionBlocked)
	}
}

func (*ADEIntegrationResource) Delete(_ context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.Diagnostics.AddError("ADE integration deletion blocked", adeDeletionBlocked)
}
func (*ADEIntegrationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if !client.ValidID(req.ID) {
		resp.Diagnostics.AddError("Invalid ADE import ID", "Import using the integration UUID.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), strings.ToLower(req.ID))...)
}
