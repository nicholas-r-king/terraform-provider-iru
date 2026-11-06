// SPDX-License-Identifier: MPL-2.0
package client

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestADETagsObservedReplacementAndExplicitEmptyList(t *testing.T) {
	for _, tags := range [][]string{{"alpha", "beta"}, {}} {
		calls := 0
		c := testClient(t, func(r *http.Request) (*http.Response, error) {
			calls++
			if r.URL.Path != adePath+"/devices/"+testID {
				t.Fatal("wrong ADE record")
			}
			if r.Method == "PATCH" {
				var fields map[string]json.RawMessage
				if json.NewDecoder(r.Body).Decode(&fields) != nil || len(fields) != 1 || string(fields["tags"]) == "null" {
					t.Fatal("tags-only write lost full-set intent")
				}
				return response(200, `{}`), nil
			}
			if r.Method != "GET" {
				t.Fatal("unexpected mutation")
			}
			var value map[string]any
			_ = json.Unmarshal([]byte(adeDeviceFixture), &value)
			value["user_id"], value["tags"] = json.Number("9007199254740993"), tags
			body, _ := json.Marshal(value)
			return response(200, string(body)), nil
		})
		value, err := c.UpdateDeviceSettings(context.Background(), testID, true, map[string]any{"tags": tags})
		if err != nil || calls != 2 || !value.TagsKnown || value.Tags == nil || len(value.Tags) != len(tags) || value.UserID == nil || *value.UserID != "9007199254740993" {
			t.Fatal("documented tags write/read lost intent or changed a legacy user observation", err)
		}
	}
}

func TestADETagsMissingNullInvalidAndMetadataProjection(t *testing.T) {
	value, err := decodeDeviceSettings([]byte(adeDeviceFixture), testID, true)
	if err != nil || value.TagsKnown || value.Tags != nil {
		t.Fatal("missing ADE tags were invented")
	}
	for _, raw := range []string{`null`, `true`, `{}`, `["duplicate","duplicate"]`, `[""]`, `[1]`} {
		body := strings.TrimSuffix(adeDeviceFixture, "}") + `,"tags":` + raw + `}`
		if _, err := decodeDeviceSettings([]byte(body), testID, true); err == nil {
			t.Fatal("invalid managed ADE tags accepted")
		}
		// The paired metadata lookup now models tags too; invalid observed
		// data is not silently presented as a known empty set.
		if _, err := decodeDeviceRecord([]byte(body), testID, true); err == nil {
			t.Fatal("metadata lookup accepted invalid observed tags")
		}
	}
}
