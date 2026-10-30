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

const inHouseAppsPath = "/api/v1/library/ipa-apps"

type InHouseAppInput struct {
	Name         string `json:"name"`
	FileKey      string `json:"file_key"`
	Active       bool   `json:"active"`
	RunsOnIphone bool   `json:"runs_on_iphone"`
	RunsOnIpad   bool   `json:"runs_on_ipad"`
	RunsOnTV     bool   `json:"runs_on_tv"`
}
type InHouseApp struct {
	ID string `json:"id"`
	InHouseAppInput
}

func (input InHouseAppInput) Validate() error {
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.FileKey) == "" || strings.IndexFunc(input.FileKey, unicode.IsControl) >= 0 {
		return errors.New("In-House App requires a nonblank name and existing uploaded file_key without control characters")
	}
	if !input.RunsOnIphone && !input.RunsOnIpad && !input.RunsOnTV {
		return errors.New("select at least one In-House App platform: iphone, ipad, or tv")
	}
	return nil
}
func decodeInHouseApp(data []byte, expected string) (InHouseApp, error) {
	var value InHouseApp
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || json.Unmarshal(data, &value) != nil {
		return value, errors.New("Iru returned invalid In-House App JSON (content withheld)")
	}
	for _, field := range []string{"id", "name", "file_key", "active", "runs_on_iphone", "runs_on_ipad", "runs_on_tv"} {
		if raw, exists := fields[field]; !exists || string(raw) == "null" {
			return value, errors.New("Iru In-House App response is missing managed fields (content withheld)")
		}
	}
	if !ValidID(value.ID) || (expected != "" && !strings.EqualFold(value.ID, expected)) || value.InHouseAppInput.Validate() != nil {
		return value, errors.New("Iru returned invalid In-House App managed fields (content withheld)")
	}
	value.ID = strings.ToLower(value.ID)
	return value, nil
}
func (c *Client) CreateInHouseApp(ctx context.Context, input InHouseAppInput) (InHouseApp, error) {
	if err := input.Validate(); err != nil {
		return InHouseApp{}, err
	}
	data, err := c.request(ctx, http.MethodPost, inHouseAppsPath, input, http.StatusCreated)
	if err != nil {
		return InHouseApp{}, err
	}
	return decodeInHouseApp(data, "")
}
func (c *Client) GetInHouseApp(ctx context.Context, id string) (InHouseApp, error) {
	if !ValidID(id) {
		return InHouseApp{}, errors.New("In-House App ID must be a UUID")
	}
	data, err := c.request(ctx, http.MethodGet, inHouseAppsPath+"/"+strings.ToLower(id), nil, http.StatusOK)
	if err != nil {
		return InHouseApp{}, err
	}
	return decodeInHouseApp(data, id)
}
func (c *Client) UpdateInHouseApp(ctx context.Context, id string, input InHouseAppInput, replaceInstaller bool) (InHouseApp, error) {
	if !ValidID(id) {
		return InHouseApp{}, errors.New("In-House App ID must be a UUID")
	}
	if err := input.Validate(); err != nil {
		return InHouseApp{}, err
	}
	fields := map[string]any{"name": input.Name, "active": input.Active, "runs_on_iphone": input.RunsOnIphone, "runs_on_ipad": input.RunsOnIpad, "runs_on_tv": input.RunsOnTV}
	if replaceInstaller {
		fields["file_key"] = input.FileKey
	}
	data, err := c.request(ctx, http.MethodPatch, inHouseAppsPath+"/"+strings.ToLower(id), fields, http.StatusOK)
	if err != nil {
		return InHouseApp{}, err
	}
	return decodeInHouseApp(data, id)
}
func (c *Client) DeleteInHouseApp(ctx context.Context, id string) error {
	if !ValidID(id) {
		return errors.New("In-House App ID must be a UUID")
	}
	_, err := c.request(ctx, http.MethodDelete, inHouseAppsPath+"/"+strings.ToLower(id), nil, http.StatusNoContent)
	if IsNotFound(err) {
		return nil
	}
	return err
}
