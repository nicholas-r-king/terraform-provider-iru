// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
)

// These resources manage a projection of an existing device. They do not own
// its enrollment or lifecycle; Delete releases management without an API DELETE.
type DeviceSettingsResource struct {
	client *client.Client
	ade    bool
}

var (
	_ resource.Resource                   = &DeviceSettingsResource{}
	_ resource.ResourceWithConfigure      = &DeviceSettingsResource{}
	_ resource.ResourceWithValidateConfig = &DeviceSettingsResource{}
	_ resource.ResourceWithModifyPlan     = &DeviceSettingsResource{}
	_ resource.ResourceWithImportState    = &DeviceSettingsResource{}
)

func NewDeviceSettingsResource() resource.Resource    { return &DeviceSettingsResource{} }
func NewADEDeviceSettingsResource() resource.Resource { return &DeviceSettingsResource{ade: true} }
func (r *DeviceSettingsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	suffix := "_device_settings"
	if r.ade {
		suffix = "_ade_device_settings"
	}
	resp.TypeName = req.ProviderTypeName + suffix
}
func (r *DeviceSettingsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	attrs := map[string]schema.Attribute{
		"id":           schema.StringAttribute{Computed: true, MarkdownDescription: "Existing device UUID. This resource does not create a device record.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"device_id":    schema.StringAttribute{Required: true, MarkdownDescription: "Existing enrolled or ADE-device record UUID, according to resource type. Changing it releases the old settings and adopts the new record.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"blueprint_id": schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "Assigned Blueprint UUID. Omit to observe without managing it."},
		"user_id":      schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "Assigned directory-user UUID. Omit to leave unmanaged. For enrolled devices only, an empty string explicitly clears assignment."},
		"asset_tag":    schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "Asset tag. Omit to leave unmanaged. For enrolled devices only, an empty string explicitly clears it."},
	}
	description := "Manages Blueprint, user, asset-tag and tag settings of an existing enrolled device through GET/PATCH. Omitted settings are observed but not written. Import by device UUID. Destroy releases management and retains the device and its settings; enrollment, erasure, deletion and commands are never performed."
	if r.ade {
		description = "Manages documented ADE-device assignment settings through GET/PATCH. This is the ADE device record UUID, not its enrolled MDM device UUID. Import by ADE-device UUID. Omitted fields remain unmanaged; Blueprint/routing choices are coupled. Destroy only releases Terraform management. No enrollment, erasure or device deletion is performed."
		attrs["blueprint_id"] = schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "ADE Blueprint UUID. A configured UUID selects that Blueprint and disables routing. An empty string selects Blueprint routing. Omit to leave unmanaged unless use_blueprint_routing is configured."}
		attrs["use_blueprint_routing"] = schema.BoolAttribute{Optional: true, Computed: true, MarkdownDescription: "Select Blueprint routing. true clears blueprint_id; false requires a configured or existing Blueprint UUID. Omit to leave unmanaged unless blueprint_id is configured."}
		attrs["override_dep_profile_id"] = schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "ADE Library Item override UUID; empty string relinks to the Blueprint/routing default. Requires the API to report this field so refresh can verify it. Omit to leave unmanaged."}
	} else {
		attrs["tags"] = schema.SetAttribute{Optional: true, Computed: true, ElementType: types.StringType, MarkdownDescription: "Assigned tag names. Omit to leave unmanaged; an explicit empty set clears all tags."}
	}
	resp.Schema = schema.Schema{MarkdownDescription: description, Attributes: attrs}
}
func (r *DeviceSettingsResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok || c == nil {
		resp.Diagnostics.AddError("Unexpected Iru client type", "Could not configure the device settings client.")
		return
	}
	r.client = c
}

type settingsObjectReader interface {
	Get(context.Context, any) diag.Diagnostics
}

func settingsObject(ctx context.Context, reader settingsObjectReader, diagnostics *diag.Diagnostics) types.Object {
	var obj types.Object
	diagnostics.Append(reader.Get(ctx, &obj)...)
	return obj
}
func settingString(obj types.Object, key string) types.String {
	if v, ok := obj.Attributes()[key].(types.String); ok {
		return v
	}
	return types.StringNull()
}
func settingBool(obj types.Object, key string) types.Bool {
	if v, ok := obj.Attributes()[key].(types.Bool); ok {
		return v
	}
	return types.BoolNull()
}
func settingsResolved(obj types.Object) bool {
	if obj.IsNull() || obj.IsUnknown() {
		return false
	}
	for _, value := range obj.Attributes() {
		if value.IsUnknown() {
			return false
		}
	}
	return true
}
func settingUUID(value *string, prior types.String) types.String {
	if value == nil {
		if !prior.IsNull() && !prior.IsUnknown() && prior.ValueString() == "" {
			return types.StringValue("")
		}
		return types.StringNull()
	}
	if !prior.IsNull() && !prior.IsUnknown() && strings.EqualFold(*value, prior.ValueString()) {
		return prior
	}
	return types.StringValue(*value)
}
func settingText(value *string, prior types.String) types.String {
	if value == nil {
		return settingUUID(nil, prior)
	}
	return types.StringValue(*value)
}
func deviceSettingsState(ctx context.Context, prior types.Object, value client.DeviceSettings, ade bool) (types.Object, diag.Diagnostics) {
	attrs := prior.Attributes()
	attrs["id"] = types.StringValue(value.ID)
	attrs["device_id"] = settingUUID(&value.ID, settingString(prior, "device_id"))
	attrs["blueprint_id"] = settingUUID(value.BlueprintID, settingString(prior, "blueprint_id"))
	attrs["user_id"] = settingUUID(value.UserID, settingString(prior, "user_id"))
	attrs["asset_tag"] = settingText(value.AssetTag, settingString(prior, "asset_tag"))
	if ade {
		attrs["use_blueprint_routing"] = types.BoolValue(*value.UseBlueprintRouting)
		if value.BlueprintID == nil {
			attrs["blueprint_id"] = types.StringValue("")
		}
		attrs["override_dep_profile_id"] = settingUUID(value.OverrideDEPProfileID, settingString(prior, "override_dep_profile_id"))
	} else {
		tags, diags := types.SetValueFrom(ctx, types.StringType, value.Tags)
		if diags.HasError() {
			return types.ObjectNull(prior.AttributeTypes(ctx)), diags
		}
		attrs["tags"] = tags
	}
	return types.ObjectValue(prior.AttributeTypes(ctx), attrs)
}
func deviceConfigDiagnostics(ctx context.Context, obj types.Object, ade bool) diag.Diagnostics {
	var diagnostics diag.Diagnostics
	if obj.IsNull() || obj.IsUnknown() {
		return diagnostics
	}
	id := settingString(obj, "device_id")
	if !id.IsNull() && !id.IsUnknown() && !client.ValidID(id.ValueString()) {
		diagnostics.AddAttributeError(path.Root("device_id"), "Invalid device UUID", "device_id must identify an existing device record by UUID.")
	}
	for _, key := range []string{"blueprint_id", "user_id", "override_dep_profile_id"} {
		value := settingString(obj, key)
		if value.IsNull() || value.IsUnknown() {
			continue
		}
		emptyAllowed := (ade && (key == "blueprint_id" || key == "override_dep_profile_id")) || (!ade && key == "user_id")
		if !(emptyAllowed && value.ValueString() == "") && !client.ValidID(value.ValueString()) {
			diagnostics.AddAttributeError(path.Root(key), "Invalid assignment value", "Use a UUID, or the documented empty-string clearing convention where supported.")
		}
	}
	if ade {
		asset := settingString(obj, "asset_tag")
		if !asset.IsNull() && !asset.IsUnknown() && strings.TrimSpace(asset.ValueString()) == "" {
			diagnostics.AddAttributeError(path.Root("asset_tag"), "ADE clearing contract unavailable", "ADE asset-tag clearing is not documented. Omit asset_tag to leave it unchanged.")
		}
		bp, route := settingString(obj, "blueprint_id"), settingBool(obj, "use_blueprint_routing")
		if !bp.IsNull() && !bp.IsUnknown() && !route.IsNull() && !route.IsUnknown() && ((bp.ValueString() == "" && !route.ValueBool()) || (bp.ValueString() != "" && route.ValueBool())) {
			diagnostics.AddError("Conflicting ADE assignment settings", "A Blueprint UUID requires use_blueprint_routing=false; routing requires an empty or omitted blueprint_id.")
		}
	}
	if !ade {
		if value, ok := obj.Attributes()["tags"].(types.Set); ok && !value.IsNull() && !value.IsUnknown() {
			tags := []string{}
			diagnostics.Append(value.ElementsAs(ctx, &tags, false)...)
			if !diagnostics.HasError() {
				if tags == nil {
					tags = []string{}
				}
				if err := client.ValidateDevicePatch(map[string]any{"tags": tags}, false); err != nil {
					diagnostics.AddAttributeError(path.Root("tags"), "Invalid device tags", err.Error())
				}
			}
		}
	}
	return diagnostics
}
func (r *DeviceSettingsResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	obj := settingsObject(ctx, req.Config, &resp.Diagnostics)
	if !resp.Diagnostics.HasError() {
		resp.Diagnostics.Append(deviceConfigDiagnostics(ctx, obj, r.ade)...)
	}
}
func (r *DeviceSettingsResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if !r.ade || req.Plan.Raw.IsNull() {
		return
	}
	cfg := settingsObject(ctx, req.Config, &resp.Diagnostics)
	planned := settingsObject(ctx, req.Plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(deviceConfigDiagnostics(ctx, cfg, r.ade)...)
	if resp.Diagnostics.HasError() {
		return
	}
	attrs := planned.Attributes()
	bp, route := settingString(cfg, "blueprint_id"), settingBool(cfg, "use_blueprint_routing")
	if !bp.IsNull() && !bp.IsUnknown() {
		if bp.ValueString() == "" {
			attrs["use_blueprint_routing"] = types.BoolValue(true)
		} else if route.IsNull() {
			attrs["use_blueprint_routing"] = types.BoolValue(false)
		}
	} else if bp.IsNull() && !route.IsNull() && !route.IsUnknown() && route.ValueBool() {
		attrs["blueprint_id"] = types.StringValue("")
	}
	value, diags := types.ObjectValue(planned.AttributeTypes(ctx), attrs)
	resp.Diagnostics.Append(diags...)
	if !resp.Diagnostics.HasError() {
		resp.Diagnostics.Append(resp.Plan.Set(ctx, value)...)
	}
}
func sameDeviceUUID(value string, current *string) bool {
	return current != nil && strings.EqualFold(value, *current)
}
func sameDeviceUUIDPointers(left, right *string) bool {
	return (left == nil && right == nil) || (left != nil && sameDeviceUUID(*left, right))
}
func desiredText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
func sameDeviceText(left, right *string) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && *left == *right)
}
func deviceSettingsPatch(ctx context.Context, cfg types.Object, current client.DeviceSettings, ade bool) (map[string]any, error) {
	fields := map[string]any{}
	bp := settingString(cfg, "blueprint_id")
	if ade {
		route := settingBool(cfg, "use_blueprint_routing")
		if !bp.IsNull() || !route.IsNull() {
			wantRouting := (!bp.IsNull() && bp.ValueString() == "") || (!route.IsNull() && route.ValueBool())
			var target *string
			if !wantRouting {
				if !bp.IsNull() {
					v := bp.ValueString()
					target = &v
				} else {
					target = current.BlueprintID
				}
				if target == nil {
					return nil, errors.New("configure blueprint_id before disabling ADE Blueprint routing")
				}
			}
			if *current.UseBlueprintRouting != wantRouting || (!wantRouting && !sameDeviceUUID(*target, current.BlueprintID)) {
				fields["use_blueprint_routing"] = wantRouting
				if target == nil {
					fields["blueprint_id"] = nil
				} else {
					fields["blueprint_id"] = *target
				}
			}
		}
		override := settingString(cfg, "override_dep_profile_id")
		if !override.IsNull() {
			if !current.OverrideKnown {
				return nil, errors.New("Iru does not report override_dep_profile_id for this record; the override cannot be safely managed")
			}
			target := desiredText(override.ValueString())
			if !sameDeviceUUIDPointers(target, current.OverrideDEPProfileID) {
				if target == nil {
					fields["override_dep_profile_id"] = nil
				} else {
					fields["override_dep_profile_id"] = *target
				}
			}
		}
	} else if !bp.IsNull() && !sameDeviceUUID(bp.ValueString(), current.BlueprintID) {
		fields["blueprint_id"] = bp.ValueString()
	}
	user := settingString(cfg, "user_id")
	if !user.IsNull() {
		target := desiredText(user.ValueString())
		same := (target == nil && current.UserID == nil) || (target != nil && sameDeviceUUID(*target, current.UserID))
		if !same {
			key := "user"
			if ade {
				key = "user_id"
			}
			if target == nil {
				fields[key] = nil
			} else {
				fields[key] = *target
			}
		}
	}
	asset := settingString(cfg, "asset_tag")
	if !asset.IsNull() {
		target := desiredText(asset.ValueString())
		if !sameDeviceText(target, current.AssetTag) {
			if target == nil {
				fields["asset_tag"] = nil
			} else {
				fields["asset_tag"] = *target
			}
		}
	}
	if !ade {
		tags := cfg.Attributes()["tags"].(types.Set)
		if !tags.IsNull() {
			values := []string{}
			if diags := tags.ElementsAs(ctx, &values, false); diags.HasError() {
				return nil, errors.New("device tag inputs are unresolved")
			}
			if values == nil {
				values = []string{}
			}
			prior := append([]string{}, current.Tags...)
			sort.Strings(values)
			sort.Strings(prior)
			same := len(values) == len(prior)
			if same {
				for i := range values {
					if values[i] != prior[i] {
						same = false
						break
					}
				}
			}
			if !same {
				fields["tags"] = values
			}
		}
	}
	if len(fields) > 0 {
		if err := client.ValidateDevicePatch(fields, ade); err != nil {
			return nil, err
		}
	}
	return fields, nil
}
func verifyDeviceOverride(prior types.Object, current client.DeviceSettings, ade bool) error {
	value := settingString(prior, "override_dep_profile_id")
	if ade && !current.OverrideKnown && !value.IsNull() && !value.IsUnknown() {
		return errors.New("Iru omitted previously known ADE override metadata; prior state was retained")
	}
	return nil
}
func (r *DeviceSettingsResource) apply(ctx context.Context, cfg, planned types.Object, diagnostics *diag.Diagnostics) (types.Object, bool) {
	if r.client == nil || !settingsResolved(cfg) {
		diagnostics.AddError("Device settings client or inputs unavailable", "Configure the provider and resolve all configured settings before applying.")
		return types.Object{}, false
	}
	diagnostics.Append(deviceConfigDiagnostics(ctx, cfg, r.ade)...)
	if diagnostics.HasError() {
		return types.Object{}, false
	}
	id := settingString(cfg, "device_id").ValueString()
	current, err := r.client.GetDeviceSettings(ctx, id, r.ade)
	if err != nil {
		diagnostics.AddError("Unable to read existing Iru device settings", err.Error())
		return types.Object{}, false
	}
	fields, err := deviceSettingsPatch(ctx, cfg, current, r.ade)
	if err != nil {
		diagnostics.AddError("Unable to manage Iru device settings", err.Error())
		return types.Object{}, false
	}
	if len(fields) > 0 {
		current, err = r.client.UpdateDeviceSettings(ctx, id, r.ade, fields)
		if err != nil {
			diagnostics.AddError("Unable to update Iru device settings", err.Error()+" A setting may have changed; refresh/reconcile before another apply.")
			return types.Object{}, false
		}
	}
	remaining, err := deviceSettingsPatch(ctx, cfg, current, r.ade)
	if err != nil || len(remaining) > 0 {
		diagnostics.AddError("Iru device settings were not confirmed", "The API did not return the configured settings after update. No write was retried; refresh/reconcile before another apply.")
		return types.Object{}, false
	}
	if err := verifyDeviceOverride(planned, current, r.ade); err != nil {
		diagnostics.AddError("Unable to verify ADE device settings", err.Error())
		return types.Object{}, false
	}
	value, diags := deviceSettingsState(ctx, planned, current, r.ade)
	diagnostics.Append(diags...)
	return value, !diagnostics.HasError()
}
func (r *DeviceSettingsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	cfg := settingsObject(ctx, req.Config, &resp.Diagnostics)
	planned := settingsObject(ctx, req.Plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if value, ok := r.apply(ctx, cfg, planned, &resp.Diagnostics); ok {
		resp.Diagnostics.Append(resp.State.Set(ctx, value)...)
	}
}
func (r *DeviceSettingsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	cfg := settingsObject(ctx, req.Config, &resp.Diagnostics)
	planned := settingsObject(ctx, req.Plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if value, ok := r.apply(ctx, cfg, planned, &resp.Diagnostics); ok {
		resp.Diagnostics.Append(resp.State.Set(ctx, value)...)
	}
}
func (r *DeviceSettingsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	prior := settingsObject(ctx, req.State, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Iru client unavailable", "Configure the provider before reading device settings.")
		return
	}
	current, err := r.client.GetDeviceSettings(ctx, settingString(prior, "device_id").ValueString(), r.ade)
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Iru device settings", err.Error())
		return
	}
	if err := verifyDeviceOverride(prior, current, r.ade); err != nil {
		resp.Diagnostics.AddError("Unable to verify ADE device settings", err.Error())
		return
	}
	value, diags := deviceSettingsState(ctx, prior, current, r.ade)
	resp.Diagnostics.Append(diags...)
	if !resp.Diagnostics.HasError() {
		resp.Diagnostics.Append(resp.State.Set(ctx, value)...)
	}
}
func (*DeviceSettingsResource) Delete(ctx context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.State.RemoveResource(ctx)
	resp.Diagnostics.AddWarning("Device settings management released", "The existing device and its settings remain in Iru. No enrollment, erase, unenrollment or device deletion operation was performed.")
}
func (*DeviceSettingsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if !client.ValidID(req.ID) {
		resp.Diagnostics.AddError("Invalid device settings import ID", "Import using the existing device record UUID.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), strings.ToLower(req.ID))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("device_id"), strings.ToLower(req.ID))...)
}

// Used by the data sources to construct a correctly typed projection without
// a resource configuration or exposing the rest of a device response.
func emptyDeviceSettingsObject(ade bool) types.Object {
	typesMap := map[string]attr.Type{"id": types.StringType, "device_id": types.StringType, "blueprint_id": types.StringType, "user_id": types.StringType, "asset_tag": types.StringType}
	values := map[string]attr.Value{}
	for key := range typesMap {
		values[key] = types.StringNull()
	}
	if ade {
		typesMap["use_blueprint_routing"] = types.BoolType
		typesMap["override_dep_profile_id"] = types.StringType
		values["use_blueprint_routing"] = types.BoolNull()
		values["override_dep_profile_id"] = types.StringNull()
	} else {
		typesMap["tags"] = types.SetType{ElemType: types.StringType}
		values["tags"] = types.SetNull(types.StringType)
	}
	return types.ObjectValueMust(typesMap, values)
}
