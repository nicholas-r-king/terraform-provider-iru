// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// DeviceSettings deliberately projects assignment metadata only. It excludes
// device names, serials, user email/name, inventory, secrets and command history.
type DeviceSettings struct {
	ID                   string
	BlueprintID          *string
	UserID               *string
	AssetTag             *string
	Tags                 []string
	UseBlueprintRouting  *bool
	OverrideDEPProfileID *string
	OverrideKnown        bool
}

func (DeviceSettings) String() string   { return "Iru device settings (private metadata withheld)" }
func (DeviceSettings) GoString() string { return "Iru device settings (private metadata withheld)" }

func deviceSettingsPath(id string, ade bool) (string, error) {
	if !ValidID(id) {
		return "", errors.New("device_id must be a UUID")
	}
	prefix := "/api/v1/devices/"
	if ade {
		prefix = adePath + "/devices/"
	}
	return prefix + strings.ToLower(id), nil
}
func nullableSetting(fields map[string]json.RawMessage, key string, uuid bool) (*string, error) {
	raw, exists := fields[key]
	if !exists {
		return nil, errors.New("Iru omitted managed device metadata (content withheld)")
	}
	if string(raw) == "null" {
		return nil, nil
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return nil, errors.New("Iru returned invalid device metadata types (content withheld)")
	}
	if value == "" {
		return nil, nil
	}
	if uuid && !ValidID(value) {
		return nil, errors.New("Iru returned a non-UUID device assignment (content withheld)")
	}
	if uuid {
		value = strings.ToLower(value)
	}
	return &value, nil
}
func decodeDeviceSettings(data []byte, id string, ade bool) (DeviceSettings, error) {
	var result DeviceSettings
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil {
		return result, errors.New("Iru returned invalid device settings JSON (content withheld)")
	}
	identity := "device_id"
	if ade {
		identity = "id"
	}
	if json.Unmarshal(fields[identity], &result.ID) != nil || !ValidID(result.ID) || !strings.EqualFold(result.ID, id) {
		return result, errors.New("Iru returned mismatched device identity (content withheld)")
	}
	result.ID = strings.ToLower(result.ID)
	var err error
	result.BlueprintID, err = nullableSetting(fields, "blueprint_id", true)
	if err != nil {
		return result, err
	}
	result.AssetTag, err = nullableSetting(fields, "asset_tag", false)
	if err != nil {
		return result, err
	}
	if ade {
		result.UserID, err = nullableSetting(fields, "user_id", true)
		if err != nil {
			return result, err
		}
		if raw, exists := fields["use_blueprint_routing"]; !exists || string(raw) == "null" || json.Unmarshal(raw, &result.UseBlueprintRouting) != nil || result.UseBlueprintRouting == nil {
			return result, errors.New("Iru omitted valid ADE Blueprint routing metadata")
		}
		if (*result.UseBlueprintRouting && result.BlueprintID != nil) || (!*result.UseBlueprintRouting && result.BlueprintID == nil) {
			return result, errors.New("Iru returned inconsistent ADE routing/Blueprint assignments")
		}
		if _, exists := fields["override_dep_profile_id"]; exists {
			result.OverrideKnown = true
			result.OverrideDEPProfileID, err = nullableSetting(fields, "override_dep_profile_id", true)
			if err != nil {
				return result, err
			}
		}
	} else {
		raw, exists := fields["user"]
		if !exists {
			return result, errors.New("Iru omitted device user assignment metadata")
		}
		if string(raw) != "null" && string(raw) != `""` {
			var user struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(raw, &user) != nil || !ValidID(user.ID) {
				return result, errors.New("Iru returned invalid device user identity (content withheld)")
			}
			value := strings.ToLower(user.ID)
			result.UserID = &value
		}
		raw, exists = fields["tags"]
		if !exists || string(raw) == "null" || json.Unmarshal(raw, &result.Tags) != nil || result.Tags == nil {
			return result, errors.New("Iru omitted a valid device tag list")
		}
		if err := validateDeviceTags(result.Tags); err != nil {
			return result, errors.New("Iru returned invalid device tags (content withheld)")
		}
	}
	return result, nil
}
func validateDeviceTags(tags []string) error {
	if len(tags) > 10000 {
		return errors.New("device tag count exceeds its bound")
	}
	seen := map[string]bool{}
	for _, tag := range tags {
		if strings.TrimSpace(tag) == "" || seen[tag] {
			return errors.New("device tags must be nonblank and unique")
		}
		seen[tag] = true
	}
	return nil
}
func ValidateDevicePatch(fields map[string]any, ade bool) error {
	if len(fields) == 0 {
		return errors.New("device settings PATCH must contain an explicit change")
	}
	for key, value := range fields {
		switch key {
		case "blueprint_id":
			if value == nil {
				if !ade || fields["use_blueprint_routing"] != true {
					return errors.New("null blueprint_id requires explicit ADE Blueprint routing")
				}
			} else if v, ok := value.(string); !ok || !ValidID(v) {
				return errors.New("blueprint_id must be a UUID")
			}
		case "user", "user_id":
			if (ade && key != "user_id") || (!ade && key != "user") {
				return errors.New("unsupported device assignment field")
			}
			if value == nil {
				if ade {
					return errors.New("ADE user clearing has no documented null contract")
				}
			} else if v, ok := value.(string); !ok || !ValidID(v) {
				return errors.New("user assignment must be a UUID")
			}
		case "asset_tag":
			if value == nil {
				if ade {
					return errors.New("ADE asset tag clearing has no documented null contract")
				}
			} else if v, ok := value.(string); !ok || (ade && strings.TrimSpace(v) == "") {
				return errors.New("asset_tag must be a string; use documented clearing semantics")
			}
		case "tags":
			if ade {
				return errors.New("ADE device tag updates are not documented")
			}
			v, ok := value.([]string)
			if !ok || v == nil {
				return errors.New("tags must be an explicit list, including [] to clear")
			}
			if err := validateDeviceTags(v); err != nil {
				return err
			}
		case "use_blueprint_routing":
			if !ade {
				return errors.New("Blueprint routing is an ADE setting")
			}
			v, ok := value.(bool)
			if !ok {
				return errors.New("use_blueprint_routing must be boolean")
			}
			bp, present := fields["blueprint_id"]
			if v && (!present || bp != nil) {
				return errors.New("ADE Blueprint routing requires blueprint_id=null")
			}
			if !v && present && bp == nil {
				return errors.New("a concrete Blueprint is required when disabling routing")
			}
		case "override_dep_profile_id":
			if !ade {
				return errors.New("ADE Library Item overrides require an ADE device")
			}
			if value != nil {
				if v, ok := value.(string); !ok || !ValidID(v) {
					return errors.New("override_dep_profile_id must be a UUID or null")
				}
			}
		default:
			return errors.New("device settings PATCH contains an undocumented field")
		}
	}
	return nil
}
func (c *Client) GetDeviceSettings(ctx context.Context, id string, ade bool) (DeviceSettings, error) {
	endpoint, err := deviceSettingsPath(id, ade)
	if err != nil {
		return DeviceSettings{}, err
	}
	data, err := c.request(ctx, http.MethodGet, endpoint, nil, http.StatusOK)
	if err != nil {
		return DeviceSettings{}, err
	}
	return decodeDeviceSettings(data, id, ade)
}
func (c *Client) UpdateDeviceSettings(ctx context.Context, id string, ade bool, fields map[string]any) (DeviceSettings, error) {
	endpoint, err := deviceSettingsPath(id, ade)
	if err != nil {
		return DeviceSettings{}, err
	}
	if err := ValidateDevicePatch(fields, ade); err != nil {
		return DeviceSettings{}, err
	}
	if _, err := c.request(ctx, http.MethodPatch, endpoint, fields, http.StatusOK); err != nil {
		return DeviceSettings{}, err
	}
	// Confirm the complete managed projection after a partial update. Writes are
	// never replayed if either the PATCH or its following read is interrupted.
	return c.GetDeviceSettings(ctx, id, ade)
}
