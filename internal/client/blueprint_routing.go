// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode"
)

const blueprintRoutingPath = "/api/v1/blueprint-routing/"

type BlueprintRouting struct {
	EnrollmentCode struct {
		Code   *string `json:"code"`
		Active *bool   `json:"is_active"`
	} `json:"enrollment_code"`
}

func (BlueprintRouting) String() string   { return "Iru Blueprint routing (enrollment code redacted)" }
func (BlueprintRouting) GoString() string { return "Iru Blueprint routing (enrollment code redacted)" }
func ValidEnrollmentCode(code string) bool {
	return strings.TrimSpace(code) != "" && strings.IndexFunc(code, unicode.IsControl) < 0
}
func decodeBlueprintRouting(data []byte) (BlueprintRouting, error) {
	var value BlueprintRouting
	if json.Unmarshal(data, &value) != nil || value.EnrollmentCode.Code == nil || value.EnrollmentCode.Active == nil || !ValidEnrollmentCode(*value.EnrollmentCode.Code) {
		return BlueprintRouting{}, errors.New("Iru returned incomplete or invalid Blueprint routing enrollment settings (content withheld)")
	}
	return value, nil
}
func (c *Client) GetBlueprintRouting(ctx context.Context) (BlueprintRouting, error) {
	data, err := c.request(ctx, http.MethodGet, blueprintRoutingPath, nil, http.StatusOK)
	if err != nil {
		return BlueprintRouting{}, err
	}
	return decodeBlueprintRouting(data)
}
func (c *Client) UpdateBlueprintRouting(ctx context.Context, code string, active bool) (BlueprintRouting, error) {
	if !ValidEnrollmentCode(code) {
		return BlueprintRouting{}, errors.New("Blueprint routing enrollment_code must be nonblank and contain no control characters")
	}
	data, err := c.request(ctx, http.MethodPatch, blueprintRoutingPath, map[string]any{"enrollment_code": map[string]any{"code": code, "is_active": active}}, http.StatusOK)
	if err != nil {
		return BlueprintRouting{}, err
	}
	return decodeBlueprintRouting(data)
}
