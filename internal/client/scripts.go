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

type ScriptInput struct {
	Name                   string  `json:"name"`
	Script                 string  `json:"script"`
	RemediationScript      string  `json:"remediation_script"`
	ExecutionFrequency     string  `json:"execution_frequency"`
	Active                 bool    `json:"active"`
	Restart                bool    `json:"restart"`
	ShowInSelfService      bool    `json:"show_in_self_service"`
	SelfServiceCategoryID  *string `json:"self_service_category_id,omitempty"`
	SelfServiceRecommended *bool   `json:"self_service_recommended,omitempty"`
}

type Script struct {
	ID string `json:"id"`
	ScriptInput
}

func ValidFrequency(value string) bool {
	switch value {
	case "once", "every_15_min", "every_day", "no_enforcement":
		return true
	}
	return false
}

func (input ScriptInput) Validate() error {
	if strings.TrimSpace(input.Name) == "" {
		return errors.New("name must not be empty or whitespace")
	}
	if strings.TrimSpace(input.Script) == "" {
		return errors.New("script must not be empty or whitespace")
	}
	if !ValidFrequency(input.ExecutionFrequency) {
		return errors.New("execution_frequency must be once, every_15_min, every_day, or no_enforcement")
	}
	if input.ExecutionFrequency == "no_enforcement" && !input.ShowInSelfService {
		return errors.New("no_enforcement requires show_in_self_service=true and self_service_category_id")
	}
	if input.ShowInSelfService && (input.SelfServiceCategoryID == nil || !ValidID(*input.SelfServiceCategoryID)) {
		return errors.New("show_in_self_service=true requires a UUID self_service_category_id")
	}
	// Explicit provider constraints: the API does not document clearing hidden
	// category metadata. Manage category/recommended only while Self Service is on.
	if !input.ShowInSelfService && (input.SelfServiceCategoryID != nil || (input.SelfServiceRecommended != nil && *input.SelfServiceRecommended)) {
		return errors.New("this provider manages self_service_category_id and self_service_recommended only when show_in_self_service=true")
	}
	return nil
}

func decodeScript(data []byte) (Script, error) {
	var script Script
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || json.Unmarshal(data, &script) != nil {
		return script, errors.New("Iru returned invalid Custom Script JSON (content withheld)")
	}
	for _, key := range []string{"id", "name", "script", "remediation_script", "execution_frequency", "active", "restart"} {
		if value, present := fields[key]; !present || string(value) == "null" {
			return script, fmt.Errorf("Iru Custom Script response is missing required field %s (content withheld)", key)
		}
	}
	if !ValidID(script.ID) || !ValidFrequency(script.ExecutionFrequency) {
		return script, errors.New("Iru Custom Script response contains an unsupported ID or execution frequency (content withheld)")
	}
	if script.ShowInSelfService && (script.SelfServiceCategoryID == nil || !ValidID(*script.SelfServiceCategoryID)) {
		return script, errors.New("Iru omitted a valid category for a Self Service script; cannot safely reconstruct state")
	}
	if script.ExecutionFrequency == "no_enforcement" && !script.ShowInSelfService {
		return script, errors.New("Iru returned no_enforcement without Self Service metadata; this legacy response cannot be safely managed or imported")
	}
	if !script.ShowInSelfService {
		script.SelfServiceCategoryID = nil
		script.SelfServiceRecommended = nil
	}
	script.ID = strings.ToLower(script.ID)
	return script, nil
}

func (c *Client) CreateScript(ctx context.Context, input ScriptInput) (Script, error) {
	if err := input.Validate(); err != nil {
		return Script{}, err
	}
	// An absent remediation script is optional on creation. Do not submit an
	// empty value to serializers that reject blank optional script fields.
	// Clearing nonempty remediation is guarded below after live API rejection.
	var remediation *string
	if input.RemediationScript != "" {
		remediation = &input.RemediationScript
	}
	payload := struct {
		ScriptInput
		RemediationScript *string `json:"remediation_script,omitempty"`
	}{ScriptInput: input, RemediationScript: remediation}
	data, err := c.request(ctx, http.MethodPost, scriptsPath, payload, http.StatusCreated)
	if err != nil {
		return Script{}, err
	}
	return decodeScript(data)
}

func (c *Client) GetScript(ctx context.Context, id string) (Script, error) {
	if !ValidID(id) {
		return Script{}, errors.New("Custom Script ID must be a UUID")
	}
	data, err := c.request(ctx, http.MethodGet, scriptsPath+"/"+strings.ToLower(id), nil, http.StatusOK)
	if err != nil {
		return Script{}, err
	}
	result, err := decodeScript(data)
	if err == nil && !strings.EqualFold(result.ID, id) {
		return Script{}, errors.New("Iru response ID does not match the requested Custom Script")
	}
	return result, err
}

func (c *Client) UpdateScript(ctx context.Context, id string, input ScriptInput, prior ...ScriptInput) (Script, error) {
	if !ValidID(id) {
		return Script{}, errors.New("Custom Script ID must be a UUID")
	}
	if err := input.Validate(); err != nil {
		return Script{}, err
	}
	if len(prior) > 1 {
		return Script{}, errors.New("script update accepts at most one prior state")
	}
	if len(prior) == 1 && prior[0].RemediationScript != "" && input.RemediationScript == "" {
		return Script{}, errors.New(RemediationClearUnsupported)
	}
	var payload any = input
	if len(prior) == 1 {
		payload = scriptChanges(input, prior[0])
	}
	data, err := c.request(ctx, http.MethodPatch, scriptsPath+"/"+strings.ToLower(id), payload, http.StatusOK)
	if err != nil {
		return Script{}, err
	}
	result, err := decodeScript(data)
	if err == nil && !strings.EqualFold(result.ID, id) {
		return Script{}, errors.New("Iru response ID does not match the updated Custom Script")
	}
	return result, err
}

// Keep a valid nonblank name as the stable metadata field. Unchanged optional
// content/flags are omitted. Unsupported remediation clearing is refused before transport.
func scriptChanges(input, prior ScriptInput) map[string]any {
	fields := map[string]any{"name": input.Name}
	if input.Script != prior.Script {
		fields["script"] = input.Script
	}
	if input.RemediationScript != prior.RemediationScript {
		fields["remediation_script"] = input.RemediationScript
	}
	if input.ExecutionFrequency != prior.ExecutionFrequency {
		fields["execution_frequency"] = input.ExecutionFrequency
	}
	if input.Active != prior.Active {
		fields["active"] = input.Active
	}
	if input.Restart != prior.Restart {
		fields["restart"] = input.Restart
	}
	if input.ShowInSelfService != prior.ShowInSelfService {
		fields["show_in_self_service"] = input.ShowInSelfService
	}
	if input.ShowInSelfService {
		if input.SelfServiceCategoryID != nil && (prior.SelfServiceCategoryID == nil || !strings.EqualFold(*input.SelfServiceCategoryID, *prior.SelfServiceCategoryID)) {
			fields["self_service_category_id"] = *input.SelfServiceCategoryID
		}
		if input.SelfServiceRecommended != nil && (prior.SelfServiceRecommended == nil || *input.SelfServiceRecommended != *prior.SelfServiceRecommended) {
			fields["self_service_recommended"] = *input.SelfServiceRecommended
		}
	}
	return fields
}

func (c *Client) DeleteScript(ctx context.Context, id string) error {
	if !ValidID(id) {
		return errors.New("Custom Script ID must be a UUID")
	}
	_, err := c.request(ctx, http.MethodDelete, scriptsPath+"/"+strings.ToLower(id), nil, http.StatusNoContent)
	if IsNotFound(err) {
		return nil
	}
	return err
}

// RemediationClearUnsupported records a live-verified API limitation, not a
// license to replace/delete a Library Item or guess undocumented null semantics.
const RemediationClearUnsupported = "Clearing existing remediation content is currently unsupported: the live Iru API rejects an empty remediation_script. Retain the current content until a verified clearing contract is available. The provider will not replace or delete the Script to clear this field."
