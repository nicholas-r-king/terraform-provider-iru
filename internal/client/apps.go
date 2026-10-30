// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

const appsPath = "/api/v1/library/custom-apps"

var sha256Pattern = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

// AppInput references an already uploaded installer; it does not own S3 files.
type AppInput struct {
	Name                   string  `json:"name"`
	FileKey                string  `json:"file_key"`
	InstallType            string  `json:"install_type"`
	InstallEnforcement     string  `json:"install_enforcement"`
	AuditScript            string  `json:"audit_script"`
	PreinstallScript       string  `json:"preinstall_script"`
	PostinstallScript      string  `json:"postinstall_script"`
	UnzipLocation          string  `json:"unzip_location"`
	Active                 bool    `json:"active"`
	Restart                bool    `json:"restart"`
	ShowInSelfService      bool    `json:"show_in_self_service"`
	SelfServiceCategoryID  *string `json:"self_service_category_id,omitempty"`
	SelfServiceRecommended *bool   `json:"self_service_recommended,omitempty"`
}

// Deliberately exclude signed file_url and upload credentials from this model.
type App struct {
	ID string `json:"id"`
	AppInput
	SHA256   *string `json:"sha256,omitempty"`
	FileSize *int64  `json:"file_size,omitempty"`
}

func (input AppInput) Validate() error {
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.FileKey) == "" || strings.IndexFunc(input.FileKey, unicode.IsControl) >= 0 {
		return errors.New("name and an existing uploaded file_key are required; file_key must contain no control characters")
	}
	if input.InstallType != "package" && input.InstallType != "zip" && input.InstallType != "image" {
		return errors.New("install_type must be package, zip, or image")
	}
	if input.InstallType == "zip" && strings.TrimSpace(input.UnzipLocation) == "" {
		return errors.New("zip installation requires unzip_location")
	}
	switch input.InstallEnforcement {
	case "install_once":
	case "continuously_enforce":
		if strings.TrimSpace(input.AuditScript) == "" {
			return errors.New("continuously_enforce requires audit_script")
		}
	case "no_enforcement":
		if !input.ShowInSelfService {
			return errors.New("no_enforcement requires Self Service and a category")
		}
	default:
		return errors.New("install_enforcement must be install_once, continuously_enforce, or no_enforcement")
	}
	if input.AuditScript != "" && input.InstallEnforcement != "continuously_enforce" {
		return errors.New("audit_script is supported only with continuously_enforce")
	}
	if input.ShowInSelfService && (input.SelfServiceCategoryID == nil || !ValidID(*input.SelfServiceCategoryID)) {
		return errors.New("Self Service requires an existing category UUID")
	}
	if !input.ShowInSelfService && (input.SelfServiceCategoryID != nil || (input.SelfServiceRecommended != nil && *input.SelfServiceRecommended)) {
		return errors.New("category and recommendation are managed only while Self Service is enabled")
	}
	return nil
}

func (input AppInput) form() url.Values {
	values := url.Values{
		"name": {input.Name}, "file_key": {input.FileKey}, "install_type": {input.InstallType}, "install_enforcement": {input.InstallEnforcement},
		"audit_script": {input.AuditScript}, "preinstall_script": {input.PreinstallScript}, "postinstall_script": {input.PostinstallScript},
		"unzip_location": {input.UnzipLocation}, "active": {strconv.FormatBool(input.Active)}, "restart": {strconv.FormatBool(input.Restart)},
		"show_in_self_service": {strconv.FormatBool(input.ShowInSelfService)},
	}
	if input.SelfServiceCategoryID != nil {
		values.Set("self_service_category_id", *input.SelfServiceCategoryID)
	}
	if input.SelfServiceRecommended != nil {
		values.Set("self_service_recommended", strconv.FormatBool(*input.SelfServiceRecommended))
	}
	return values
}

func decodeApp(data []byte) (App, error) {
	var result App
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || json.Unmarshal(data, &result) != nil {
		return result, errors.New("Iru returned invalid Custom App JSON (content withheld)")
	}
	for _, key := range []string{"id", "name", "file_key", "install_type", "install_enforcement", "audit_script", "preinstall_script", "postinstall_script", "active", "restart"} {
		if value, ok := fields[key]; !ok || string(value) == "null" {
			return result, fmt.Errorf("Iru Custom App response is missing required field %s (content withheld)", key)
		}
	}
	if !ValidID(result.ID) {
		return result, errors.New("Iru returned an invalid Custom App UUID")
	}
	if !result.ShowInSelfService {
		result.SelfServiceCategoryID, result.SelfServiceRecommended = nil, nil
	}
	if err := result.AppInput.Validate(); err != nil {
		return result, fmt.Errorf("Iru returned unsupported Custom App metadata: %w", err)
	}
	if result.SHA256 != nil {
		if !sha256Pattern.MatchString(*result.SHA256) {
			return result, errors.New("Iru returned an invalid installer SHA256 (content withheld)")
		}
		value := strings.ToLower(*result.SHA256)
		result.SHA256 = &value
	}
	if result.FileSize != nil && *result.FileSize < 0 {
		return result, errors.New("Iru returned an invalid installer file size")
	}
	result.ID = strings.ToLower(result.ID)
	return result, nil
}

func (c *Client) CreateApp(ctx context.Context, input AppInput) (App, error) {
	if err := input.Validate(); err != nil {
		return App{}, err
	}
	form := input.form()
	// Optional empty script/location fields are omitted at create. PATCH can
	// still send changed empty values to request their removal explicitly.
	for _, key := range []string{"audit_script", "preinstall_script", "postinstall_script", "unzip_location"} {
		if form.Get(key) == "" {
			form.Del(key)
		}
	}
	data, err := c.requestEncoded(ctx, http.MethodPost, appsPath, []byte(form.Encode()), "application/x-www-form-urlencoded", http.StatusCreated)
	if err != nil {
		return App{}, err
	}
	return decodeApp(data)
}

func (c *Client) GetApp(ctx context.Context, id string) (App, error) {
	if !ValidID(id) {
		return App{}, errors.New("Custom App ID must be a UUID")
	}
	data, err := c.request(ctx, http.MethodGet, appsPath+"/"+strings.ToLower(id), nil, http.StatusOK)
	if err != nil {
		return App{}, err
	}
	result, err := decodeApp(data)
	if err == nil && !strings.EqualFold(result.ID, id) {
		return App{}, errors.New("Iru response ID does not match the requested Custom App")
	}
	return result, err
}

func (c *Client) UpdateApp(ctx context.Context, id string, input AppInput, replaceInstaller bool, prior ...AppInput) (App, error) {
	if !ValidID(id) {
		return App{}, errors.New("Custom App ID must be a UUID")
	}
	if err := input.Validate(); err != nil {
		return App{}, err
	}
	if len(prior) > 1 {
		return App{}, errors.New("app update accepts at most one prior state")
	}
	form := input.form()
	if len(prior) == 1 {
		old := prior[0].form()
		for key := range form {
			if key != "name" && key != "file_key" && form.Get(key) == old.Get(key) {
				form.Del(key)
			}
		}
	}
	if !replaceInstaller {
		form.Del("file_key")
	}
	data, err := c.requestEncoded(ctx, http.MethodPatch, appsPath+"/"+strings.ToLower(id), []byte(form.Encode()), "application/x-www-form-urlencoded", http.StatusOK)
	if err != nil {
		return App{}, err
	}
	result, err := decodeApp(data)
	if err == nil && !strings.EqualFold(result.ID, id) {
		return App{}, errors.New("Iru response ID does not match the updated Custom App")
	}
	return result, err
}

func (c *Client) DeleteApp(ctx context.Context, id string) error {
	if !ValidID(id) {
		return errors.New("Custom App ID must be a UUID")
	}
	_, err := c.request(ctx, http.MethodDelete, appsPath+"/"+strings.ToLower(id), nil, http.StatusNoContent)
	if IsNotFound(err) {
		return nil
	}
	return err
}
