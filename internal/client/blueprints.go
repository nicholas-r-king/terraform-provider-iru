// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const blueprintsPath = "/api/v1/blueprints"

type BlueprintInput struct {
	Name, Type, Description, Color, Icon string
	EnrollmentActive                     bool
	EnrollmentCode                       *string
	SourceType, SourceID                 string
}

type ManagedBlueprint struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Type           string `json:"type"`
	Description    string `json:"description"`
	Color          string `json:"color"`
	Icon           string `json:"icon"`
	EnrollmentCode struct {
		Code   *string `json:"code"`
		Active *bool   `json:"is_active"`
	} `json:"enrollment_code"`
}

func (input BlueprintInput) Validate() error {
	if strings.TrimSpace(input.Name) == "" {
		return errors.New("Blueprint name must not be blank")
	}
	if input.Type != "classic" && input.Type != "map" {
		return errors.New("Blueprint type must be classic or map")
	}
	if input.Type == "map" && (input.Color != "" || input.Icon != "") {
		return errors.New("color and icon are only configurable for classic Blueprints")
	}
	if (input.SourceType == "") != (input.SourceID == "") {
		return errors.New("source_type and source_id must be set together")
	}
	if input.SourceType != "" && input.SourceType != "template" && input.SourceType != "blueprint" {
		return errors.New("source_type must be template or blueprint")
	}
	if input.SourceType == "blueprint" && !ValidID(input.SourceID) {
		return errors.New("Blueprint source_id must be a UUID")
	}
	if input.SourceType == "template" {
		if n, err := strconv.ParseUint(input.SourceID, 10, 64); err != nil || n == 0 {
			return errors.New("template source_id must be a positive integer")
		}
	}
	if input.EnrollmentCode != nil {
		code := *input.EnrollmentCode
		if len(code) != 6 || strings.IndexFunc(code, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
			return errors.New("enrollment_code must contain six digits")
		}
	}
	return nil
}

func decodeManagedBlueprint(data []byte, expected string) (ManagedBlueprint, error) {
	var value ManagedBlueprint
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || json.Unmarshal(data, &value) != nil {
		return value, errors.New("Iru returned invalid Blueprint JSON (content withheld)")
	}
	for _, field := range []string{"id", "name", "type", "description", "color", "icon", "enrollment_code"} {
		if raw, exists := fields[field]; !exists || string(raw) == "null" {
			return value, errors.New("Iru Blueprint response is missing managed fields (content withheld)")
		}
	}
	if !ValidID(value.ID) || (expected != "" && !strings.EqualFold(value.ID, expected)) || strings.TrimSpace(value.Name) == "" || (value.Type != "classic" && value.Type != "map") || value.EnrollmentCode.Code == nil || value.EnrollmentCode.Active == nil {
		return value, errors.New("Iru returned invalid Blueprint managed fields (content withheld)")
	}
	value.ID = strings.ToLower(value.ID)
	return value, nil
}

func blueprintForm(input BlueprintInput, create bool) url.Values {
	v := url.Values{"name": {input.Name}, "description": {input.Description}, "enrollment_code.is_active": {strconv.FormatBool(input.EnrollmentActive)}}
	if input.EnrollmentCode != nil {
		v.Set("enrollment_code.code", *input.EnrollmentCode)
	}
	if input.Color != "" {
		v.Set("color", input.Color)
	}
	if input.Icon != "" {
		v.Set("icon", input.Icon)
	}
	if create {
		v.Set("type", input.Type)
		if input.SourceType != "" {
			v.Set("source.type", input.SourceType)
			v.Set("source.id", input.SourceID)
		}
	}
	return v
}

func (c *Client) CreateBlueprint(ctx context.Context, input BlueprintInput) (ManagedBlueprint, error) {
	if err := input.Validate(); err != nil {
		return ManagedBlueprint{}, err
	}
	data, err := c.requestEncoded(ctx, http.MethodPost, blueprintsPath, []byte(blueprintForm(input, true).Encode()), "application/x-www-form-urlencoded", http.StatusCreated)
	if err != nil {
		return ManagedBlueprint{}, err
	}
	return decodeManagedBlueprint(data, "")
}

func (c *Client) GetManagedBlueprint(ctx context.Context, id string) (ManagedBlueprint, error) {
	if !ValidID(id) {
		return ManagedBlueprint{}, errors.New("Blueprint ID must be a UUID")
	}
	data, err := c.request(ctx, http.MethodGet, blueprintsPath+"/"+strings.ToLower(id), nil, http.StatusOK)
	if err != nil {
		return ManagedBlueprint{}, err
	}
	return decodeManagedBlueprint(data, id)
}

func (c *Client) UpdateBlueprint(ctx context.Context, id string, input BlueprintInput) (ManagedBlueprint, error) {
	if !ValidID(id) {
		return ManagedBlueprint{}, errors.New("Blueprint ID must be a UUID")
	}
	if err := input.Validate(); err != nil {
		return ManagedBlueprint{}, err
	}
	_, err := c.requestEncoded(ctx, http.MethodPatch, blueprintsPath+"/"+strings.ToLower(id), []byte(blueprintForm(input, false).Encode()), "application/x-www-form-urlencoded", http.StatusOK)
	if err != nil {
		return ManagedBlueprint{}, err
	}
	// The documented PATCH response can omit type. Read the complete object;
	// never invent its type from the requested values.
	return c.GetManagedBlueprint(ctx, id)
}

func (c *Client) DeleteBlueprint(ctx context.Context, id string) error {
	if !ValidID(id) {
		return errors.New("Blueprint ID must be a UUID")
	}
	_, err := c.request(ctx, http.MethodDelete, blueprintsPath+"/"+strings.ToLower(id), nil, http.StatusNoContent)
	if IsNotFound(err) {
		return nil
	}
	return err
}
