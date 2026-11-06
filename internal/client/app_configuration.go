// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode"
)

// AppConfiguration observes supported metadata without managed-resource defaults
// or write-side constraints. No signed file URL, upload credential or binary is
// represented. Missing/null optional metadata remains nil.
type AppConfiguration struct {
	ID                     string  `json:"id"`
	Name                   string  `json:"name"`
	FileKey                string  `json:"file_key"`
	InstallType            string  `json:"install_type"`
	InstallEnforcement     string  `json:"install_enforcement"`
	AuditScript            string  `json:"audit_script"`
	PreinstallScript       string  `json:"preinstall_script"`
	PostinstallScript      string  `json:"postinstall_script"`
	Active                 bool    `json:"active"`
	Restart                bool    `json:"restart"`
	UnzipLocation          *string `json:"unzip_location"`
	ShowInSelfService      *bool   `json:"show_in_self_service"`
	SelfServiceCategoryID  *string `json:"self_service_category_id"`
	SelfServiceRecommended *bool   `json:"self_service_recommended"`
	SHA256                 *string `json:"sha256"`
	FileSize               *int64  `json:"file_size"`
}

func (c *Client) GetAppConfiguration(ctx context.Context, id string) (AppConfiguration, error) {
	var result AppConfiguration
	if !ValidID(id) {
		return result, errors.New("Custom App ID must be a UUID")
	}
	data, err := c.request(ctx, http.MethodGet, appsPath+"/"+strings.ToLower(id), nil, http.StatusOK)
	if err != nil {
		return result, err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || json.Unmarshal(data, &result) != nil {
		return AppConfiguration{}, errors.New("Iru returned invalid Custom App configuration JSON (content withheld)")
	}
	for _, key := range []string{"id", "name", "file_key", "install_type", "install_enforcement", "audit_script", "preinstall_script", "postinstall_script", "active", "restart"} {
		if value, present := fields[key]; !present || string(value) == "null" {
			return AppConfiguration{}, fmt.Errorf("Iru Custom App configuration is missing required field %s (content withheld)", key)
		}
	}
	if !ValidID(result.ID) || !strings.EqualFold(result.ID, id) {
		return AppConfiguration{}, errors.New("Iru returned an invalid or mismatched Custom App configuration identity (content withheld)")
	}
	if strings.TrimSpace(result.FileKey) == "" || strings.IndexFunc(result.FileKey, unicode.IsControl) >= 0 {
		return AppConfiguration{}, errors.New("Iru returned an unsupported installer key (content withheld)")
	}
	switch result.InstallType {
	case "package", "zip", "image":
	default:
		return AppConfiguration{}, errors.New("Iru returned an unsupported Custom App installation type (content withheld)")
	}
	switch result.InstallEnforcement {
	case "install_once", "continuously_enforce", "no_enforcement":
	default:
		return AppConfiguration{}, errors.New("Iru returned unsupported Custom App enforcement (content withheld)")
	}
	if result.SelfServiceCategoryID != nil && !ValidID(*result.SelfServiceCategoryID) {
		return AppConfiguration{}, errors.New("Iru returned an unsupported Self Service category identity (content withheld)")
	}
	if result.SHA256 != nil && !sha256Pattern.MatchString(*result.SHA256) {
		return AppConfiguration{}, errors.New("Iru returned an invalid installer SHA256 (content withheld)")
	}
	if result.FileSize != nil && *result.FileSize < 0 {
		return AppConfiguration{}, errors.New("Iru returned an invalid installer file size (content withheld)")
	}
	result.ID = strings.ToLower(result.ID)
	return result, nil
}
