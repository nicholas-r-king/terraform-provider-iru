// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// ScriptConfiguration observes response fields without the managed resource's
// defaults or hidden Self Service cleanup. Missing/null optional metadata stays
// nil; script bytes are never trimmed or normalized.
type ScriptConfiguration struct {
	ID                     string  `json:"id"`
	Name                   string  `json:"name"`
	Script                 string  `json:"script"`
	RemediationScript      string  `json:"remediation_script"`
	ExecutionFrequency     string  `json:"execution_frequency"`
	Active                 bool    `json:"active"`
	Restart                bool    `json:"restart"`
	ShowInSelfService      *bool   `json:"show_in_self_service"`
	SelfServiceCategoryID  *string `json:"self_service_category_id"`
	SelfServiceRecommended *bool   `json:"self_service_recommended"`
}

func (c *Client) GetScriptConfiguration(ctx context.Context, id string) (ScriptConfiguration, error) {
	var result ScriptConfiguration
	if !ValidID(id) {
		return result, errors.New("Custom Script ID must be a UUID")
	}
	data, err := c.request(ctx, http.MethodGet, scriptsPath+"/"+strings.ToLower(id), nil, http.StatusOK)
	if err != nil {
		return result, err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || json.Unmarshal(data, &result) != nil {
		return ScriptConfiguration{}, errors.New("Iru returned invalid Custom Script configuration JSON (content withheld)")
	}
	for _, key := range []string{"id", "name", "script", "remediation_script", "execution_frequency", "active", "restart"} {
		if value, present := fields[key]; !present || string(value) == "null" {
			return ScriptConfiguration{}, fmt.Errorf("Iru Custom Script configuration is missing required field %s (content withheld)", key)
		}
	}
	if !ValidID(result.ID) || !strings.EqualFold(result.ID, id) || !ValidFrequency(result.ExecutionFrequency) {
		return ScriptConfiguration{}, errors.New("Iru returned an unsupported or mismatched Custom Script configuration identity/frequency (content withheld)")
	}
	if result.SelfServiceCategoryID != nil && !ValidID(*result.SelfServiceCategoryID) {
		return ScriptConfiguration{}, errors.New("Iru returned an unsupported Self Service category identity (content withheld)")
	}
	result.ID = strings.ToLower(result.ID)
	return result, nil
}
