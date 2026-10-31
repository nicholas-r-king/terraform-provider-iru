// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const deviceFixture = `{"device_id":"11111111-2222-4333-8444-555555555555","blueprint_id":"aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee","user":{"id":"aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee","email":"synthetic-private-email"},"asset_tag":"synthetic-asset","tags":["one","two"],"serial_number":"synthetic-private-serial","secrets":"synthetic-private-secret"}`
const adeDeviceFixture = `{"id":"11111111-2222-4333-8444-555555555555","blueprint_id":"aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee","user_id":null,"asset_tag":null,"use_blueprint_routing":false,"override_dep_profile_id":null,"serial_number":"synthetic-private-serial"}`

func TestDeviceSettingsProjectionAndPartialPatch(t *testing.T) {
	methods := []string{}
	c := testClient(t, func(req *http.Request) (*http.Response, error) {
		methods = append(methods, req.Method)
		if req.URL.Path != "/api/v1/devices/"+testID {
			t.Fatal("wrong device endpoint")
		}
		if req.Method == "PATCH" {
			var body map[string]any
			if json.NewDecoder(req.Body).Decode(&body) != nil || len(body) != 1 || body["asset_tag"] != "synthetic-updated" {
				t.Fatal("partial update included unmanaged fields")
			}
			return response(200, `{}`), nil
		}
		body := deviceFixture
		if len(methods) > 1 {
			body = strings.Replace(body, "synthetic-asset", "synthetic-updated", 1)
		}
		return response(200, body), nil
	})
	ctx := context.Background()
	v, err := c.GetDeviceSettings(ctx, testID, false)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(v)
	if strings.Contains(string(encoded), "synthetic-private") {
		t.Fatal("projection retained unrelated device data")
	}
	v, err = c.UpdateDeviceSettings(ctx, testID, false, map[string]any{"asset_tag": "synthetic-updated"})
	if err != nil || *v.AssetTag != "synthetic-updated" || strings.Join(methods, ",") != "GET,PATCH,GET" {
		t.Fatalf("partial update/read verification failed: %v", err)
	}
}
func TestDeviceSettingsDocumentedClearing(t *testing.T) {
	c := testClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Method == "PATCH" {
			var fields map[string]json.RawMessage
			_ = json.NewDecoder(req.Body).Decode(&fields)
			if len(fields) != 3 || string(fields["user"]) != "null" || string(fields["asset_tag"]) != "null" || string(fields["tags"]) != "[]" {
				t.Fatal("clearing lost null/empty-list intent")
			}
			return response(200, `{}`), nil
		}
		return response(200, `{"device_id":"`+testID+`","blueprint_id":"`+testCategory+`","user":"","asset_tag":"","tags":[]}`), nil
	})
	v, err := c.UpdateDeviceSettings(context.Background(), testID, false, map[string]any{"user": nil, "asset_tag": nil, "tags": []string{}})
	if err != nil || v.UserID != nil || v.AssetTag != nil || len(v.Tags) != 0 {
		t.Fatalf("clear/read failed: %v", err)
	}
}
func TestADEDeviceSettingsRoutingAndOverrideContracts(t *testing.T) {
	requests := 0
	c := testClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		if req.URL.Path != adePath+"/devices/"+testID {
			t.Fatal("ADE record used wrong endpoint")
		}
		if req.Method == "PATCH" {
			var fields map[string]any
			_ = json.NewDecoder(req.Body).Decode(&fields)
			if fields["blueprint_id"] != nil || fields["use_blueprint_routing"] != true || len(fields) != 2 {
				t.Fatal("ADE routing fields not coupled")
			}
			return response(200, `{}`), nil
		}
		return response(200, strings.Replace(strings.Replace(adeDeviceFixture, `"blueprint_id":"`+testCategory+`"`, `"blueprint_id":null`, 1), `"use_blueprint_routing":false`, `"use_blueprint_routing":true`, 1)), nil
	})
	v, err := c.UpdateDeviceSettings(context.Background(), testID, true, map[string]any{"blueprint_id": nil, "use_blueprint_routing": true})
	if err != nil || v.BlueprintID != nil || !*v.UseBlueprintRouting || !v.OverrideKnown || v.OverrideDEPProfileID != nil || requests != 2 {
		t.Fatalf("ADE routing/read failed: %v", err)
	}
	v, err = decodeDeviceSettings([]byte(strings.Replace(adeDeviceFixture, `,"override_dep_profile_id":null`, "", 1)), testID, true)
	if err != nil || v.OverrideKnown {
		t.Fatal("omitted ADE override was invented")
	}
}
func TestDeviceSettingsRejectsUndocumentedWritesBeforeTransport(t *testing.T) {
	c := testClient(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid settings reached network")
		return nil, nil
	})
	for _, fields := range []map[string]any{{}, {"command": "erase"}, {"device_id": testID}, {"blueprint_id": nil}, {"user": 7}, {"user_id": testCategory}, {"tags": nil}, {"tags": []string{"duplicate", "duplicate"}}} {
		if _, err := c.UpdateDeviceSettings(context.Background(), testID, false, fields); err == nil {
			t.Fatal("invalid enrolled-device patch accepted")
		}
	}
	for _, fields := range []map[string]any{{"tags": []string{}}, {"user_id": nil}, {"asset_tag": nil}, {"use_blueprint_routing": true}, {"blueprint_id": nil, "use_blueprint_routing": false}, {"override_dep_profile_id": "not-a-uuid"}} {
		if _, err := c.UpdateDeviceSettings(context.Background(), testID, true, fields); err == nil {
			t.Fatal("unsupported ADE patch accepted")
		}
	}
	if _, err := c.GetDeviceSettings(context.Background(), "../devices", false); err == nil {
		t.Fatal("invalid device ID accepted")
	}
}

func TestADEMetadataPreservesLegacyNumericUserWithoutEnablingWrites(t *testing.T) {
	for _, legacy := range []string{"42", "9007199254740993", "18446744073709551615"} {
		body := strings.Replace(adeDeviceFixture, `"user_id":null`, `"user_id":`+legacy, 1)
		calls := 0
		c := testClient(t, func(req *http.Request) (*http.Response, error) {
			calls++
			if req.Method != http.MethodGet || req.URL.Path != adePath+"/devices/"+testID {
				t.Fatal("metadata lookup made a write or used the wrong ADE identity")
			}
			return response(200, body), nil
		})
		v, err := c.GetDeviceMetadata(context.Background(), testID, true)
		if err != nil || v.UserID == nil || *v.UserID != legacy || calls != 1 {
			t.Fatalf("legacy metadata identity was lost: %v", err)
		}
		if _, err := c.GetDeviceSettings(context.Background(), testID, true); err == nil {
			t.Fatal("legacy identity silently enabled settings management")
		}
		if _, err := c.UpdateDeviceSettings(context.Background(), testID, true, map[string]any{"user_id": legacy}); err == nil || calls != 2 {
			t.Fatal("legacy user identity reached PATCH")
		}
	}
	for _, invalid := range []string{`0`, `-1`, `1.5`, `1e2`, `18446744073709551616`, `"42"`, `true`, `{}`} {
		body := strings.Replace(adeDeviceFixture, `"user_id":null`, `"user_id":`+invalid, 1)
		if _, err := decodeDeviceRecord([]byte(body), testID, true, true); err == nil {
			t.Fatal("invalid or unverified ADE identity form accepted")
		}
	}
}
func TestDeviceSettingsWriteErrorsNeverReplay(t *testing.T) {
	for _, status := range []int{400, 403, 429, 503} {
		calls := 0
		c := testClient(t, func(*http.Request) (*http.Response, error) {
			calls++
			return response(status, `{"asset_tag":["synthetic-private-error"]}`), nil
		})
		_, err := c.UpdateDeviceSettings(context.Background(), testID, false, map[string]any{"asset_tag": "synthetic"})
		if err == nil || calls != 1 || strings.Contains(err.Error(), "synthetic-private") {
			t.Fatal("device patch retried or exposed response")
		}
	}
}
func TestDeviceSettingsMalformedReadsRetainUncertainty(t *testing.T) {
	for _, body := range []string{`{}`, strings.Replace(deviceFixture, `"tags":["one","two"]`, `"tags":null`, 1), strings.Replace(deviceFixture, `"user":{"id":"`+testCategory+`","email":"synthetic-private-email"}`, `"user":7`, 1), strings.Replace(deviceFixture, testID, testCategory, 1)} {
		if _, err := decodeDeviceSettings([]byte(body), testID, false); err == nil {
			t.Fatal("invalid device projection accepted")
		}
	}
	for _, body := range []string{strings.Replace(adeDeviceFixture, `"use_blueprint_routing":false`, `"use_blueprint_routing":null`, 1), strings.Replace(adeDeviceFixture, `"use_blueprint_routing":false`, `"use_blueprint_routing":true`, 1)} {
		if _, err := decodeDeviceSettings([]byte(body), testID, true); err == nil {
			t.Fatal("invalid ADE routing read accepted")
		}
	}
}
