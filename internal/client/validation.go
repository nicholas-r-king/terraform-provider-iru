// SPDX-License-Identifier: MPL-2.0

package client

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
)

const validationResponseLimit = 64 * 1024
const validationIssueLimit = 16

// Values stored here are selected from source-code constants only. Neither
// arbitrary JSON keys nor server messages can become an error's contents.
type validationIssue struct{ field, code string }

func managedValidationEndpoint(endpoint, collection string) bool {
	if endpoint == collection {
		return true
	}
	id, found := strings.CutPrefix(endpoint, collection+"/")
	return found && ValidID(id)
}

// Canonical field names are confined to the documented managed write schemas.
// Separate endpoint lists prevent unrelated response keys being reported as
// validation failures for a different resource.
func validationFields(endpoint string) map[string]string {
	var fields []string
	switch {
	case endpoint == appsPath+"/upload":
		fields = []string{"name"}
	case endpoint == inHouseAppsPath+"/upload":
		fields = []string{"filename"}
	case managedValidationEndpoint(endpoint, scriptsPath):
		fields = []string{"name", "script", "remediation_script", "execution_frequency", "active", "restart", "show_in_self_service", "self_service_category_id", "self_service_recommended"}
	case managedValidationEndpoint(endpoint, appsPath):
		fields = []string{"name", "file_key", "install_type", "install_enforcement", "audit_script", "preinstall_script", "postinstall_script", "unzip_location", "active", "restart", "show_in_self_service", "self_service_category_id", "self_service_recommended"}
	case managedValidationEndpoint(endpoint, profilesPath):
		fields = []string{"name", "file", "active", "runs_on_mac", "runs_on_iphone", "runs_on_ipad", "runs_on_tv", "runs_on_vision"}
	case managedValidationEndpoint(endpoint, blueprintsPath):
		fields = []string{"name", "type", "description", "color", "icon", "enrollment_code.code", "enrollment_code.is_active", "source.type", "source.id"}
	case managedValidationEndpoint(endpoint, tagsPath):
		fields = []string{"name"}
	case managedValidationEndpoint(endpoint, inHouseAppsPath):
		fields = []string{"name", "file_key", "active", "runs_on_iphone", "runs_on_ipad", "runs_on_tv"}
	case endpoint == adePath+"/" || managedValidationEndpoint(endpoint, adePath) || (strings.HasPrefix(endpoint, adePath+"/") && strings.HasSuffix(endpoint, "/renew") && ValidID(strings.TrimSuffix(strings.TrimPrefix(endpoint, adePath+"/"), "/renew"))):
		fields = []string{"blueprint_id", "use_blueprint_routing", "phone", "email", "file"}
	case strings.HasPrefix(endpoint, "/api/v1/devices/"):
		parts := strings.Split(strings.TrimPrefix(endpoint, "/api/v1/devices/"), "/")
		if (len(parts) != 2 && len(parts) != 3) || !ValidID(parts[0]) || parts[1] != "notes" || (len(parts) == 3 && !ValidID(parts[2])) {
			return nil
		}
		fields = []string{"content"}
	default:
		return nil
	}
	allowed := map[string]string{"non_field_errors": "request"}
	for _, field := range fields {
		allowed[field] = field
	}
	return allowed
}

// Only an explicit code property may supply a more specific classification.
// Message-only or unknown-code errors are classified locally as "rejected";
// free-form English messages are never guessed, echoed, or retained.
func validationCode(value any) string {
	code, _ := value.(string)
	switch code {
	case "required":
		return "required"
	case "blank":
		return "blank"
	case "null":
		return "null"
	case "invalid":
		return "invalid"
	case "invalid_choice":
		return "invalid_choice"
	case "invalid_type":
		return "invalid_type"
	case "max_length":
		return "max_length"
	case "min_length":
		return "min_length"
	case "max_value":
		return "max_value"
	case "min_value":
		return "min_value"
	case "unique":
		return "unique"
	default:
		return "rejected"
	}
}

func readValidationIssues(body io.Reader, method, endpoint string, status int) []validationIssue {
	if body == nil || (method != http.MethodPost && method != http.MethodPatch) ||
		(status != http.StatusBadRequest && status != http.StatusUnprocessableEntity) {
		return nil
	}
	allowed := validationFields(endpoint)
	if allowed == nil {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(body, validationResponseLimit+1))
	if err != nil || len(data) > validationResponseLimit || !json.Valid(data) {
		return nil
	}
	var value any
	if json.Unmarshal(data, &value) != nil {
		return nil
	}
	set := map[validationIssue]bool{}
	add := func(field string, code any) {
		if canonical, ok := allowed[field]; ok {
			set[validationIssue{canonical, validationCode(code)}] = true
		}
	}
	// Field-map, errors-wrapper and field/code-list shapes are accepted as
	// optional diagnostics, not required API response contracts. Traversal is
	// restricted to these shapes; request/value/message subtrees are ignored.
	nodes := 0
	var fieldErrors func(string, any, int)
	fieldErrors = func(field string, item any, depth int) {
		nodes++
		if nodes > 256 || depth > 4 {
			return
		}
		switch item := item.(type) {
		case []any:
			for _, entry := range item {
				fieldErrors(field, entry, depth+1)
			}
		case map[string]any:
			if code, ok := item["code"]; ok {
				add(field, code)
			} else if _, ok := item["message"]; ok {
				add(field, nil)
			}
		case string:
			if item != "" {
				add(field, nil)
			}
		}
	}
	var errorsAt func(any, int)
	errorsAt = func(item any, depth int) {
		nodes++
		if nodes > 256 || depth > 4 {
			return
		}
		switch item := item.(type) {
		case []any:
			for _, entry := range item {
				errorsAt(entry, depth+1)
			}
		case map[string]any:
			if field, ok := item["field"].(string); ok {
				if code, ok := item["code"]; ok {
					add(field, code)
				} else if _, ok := item["message"]; ok {
					add(field, nil)
				}
			}
			// Iterate constant allowlisted fields, not attacker-controlled keys.
			for field := range allowed {
				if entry, ok := item[field]; ok {
					fieldErrors(field, entry, depth+1)
				}
			}
			if nested, ok := item["errors"]; ok {
				errorsAt(nested, depth+1)
			}
		}
	}
	errorsAt(value, 0)
	if nodes > 256 {
		return nil
	}
	issues := make([]validationIssue, 0, len(set))
	for issue := range set {
		issues = append(issues, issue)
	}
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].field != issues[j].field {
			return issues[i].field < issues[j].field
		}
		return issues[i].code < issues[j].code
	})
	if len(issues) > validationIssueLimit {
		issues = issues[:validationIssueLimit]
	}
	return issues
}
