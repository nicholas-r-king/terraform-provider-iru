// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func appFixture() AppInput {
	return AppInput{Name: "Synthetic app", FileKey: "companies/synthetic/library/custom_apps/synthetic.pkg", InstallType: "package", InstallEnforcement: "install_once"}
}

func appJSON(t *testing.T, input AppInput) string {
	t.Helper()
	data, err := json.Marshal(App{ID: testID, AppInput: input})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestAppWireContractAndMetadataOnlyUpdate(t *testing.T) {
	ctx := context.Background()
	input := appFixture()
	methods := []string{"POST", "GET", "PATCH", "PATCH", "DELETE"}
	index := 0
	c := testClient(t, func(req *http.Request) (*http.Response, error) {
		if index >= len(methods) || req.Method != methods[index] || !strings.HasPrefix(req.URL.Path, appsPath) || req.Header.Get("Authorization") != "Bearer "+testToken {
			t.Fatal("unexpected app request")
		}
		step := index
		index++
		if req.Method == "POST" || req.Method == "PATCH" {
			if req.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || req.ParseForm() != nil {
				t.Fatal("app writes must use documented form encoding")
			}
			if req.Form.Get("active") != "false" || req.Form.Get("restart") != "false" || req.Form.Get("preinstall_script") != "" {
				t.Fatal("false and empty app fields must be sent")
			}
			if _, ok := req.Form["file_key"]; ok == (step == 2) {
				t.Fatal("metadata-only updates must omit file_key; replacement must send it")
			}
			if _, ok := req.Form["self_service_category_id"]; ok {
				t.Fatal("disabled Self Service must omit hidden category metadata")
			}
		}
		status := 200
		if req.Method == "POST" {
			status = 201
		}
		if req.Method == "DELETE" {
			status = 204
		}
		return response(status, appJSON(t, input)), nil
	})
	if _, err := c.CreateApp(ctx, input); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetApp(ctx, strings.ToUpper(testID)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UpdateApp(ctx, testID, input, false); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UpdateApp(ctx, testID, input, true); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteApp(ctx, testID); err != nil || index != len(methods) {
		t.Fatal("app lifecycle incomplete", err)
	}
}

func TestAppConstraintsRejectBeforeTransport(t *testing.T) {
	cases := []func(*AppInput){
		func(a *AppInput) { a.Name = " " }, func(a *AppInput) { a.FileKey = "" }, func(a *AppInput) { a.FileKey = "bad\nkey" },
		func(a *AppInput) { a.InstallType = "exe" }, func(a *AppInput) { a.InstallType = "zip" },
		func(a *AppInput) { a.InstallEnforcement = "every_day" }, func(a *AppInput) { a.InstallEnforcement = "continuously_enforce" },
		func(a *AppInput) { a.AuditScript = "synthetic" }, func(a *AppInput) { a.InstallEnforcement = "no_enforcement" },
		func(a *AppInput) { a.ShowInSelfService = true }, func(a *AppInput) { id := testCategory; a.SelfServiceCategoryID = &id },
	}
	for i, change := range cases {
		input := appFixture()
		change(&input)
		c := testClient(t, func(*http.Request) (*http.Response, error) { t.Fatal("invalid app reached transport"); return nil, nil })
		if _, err := c.CreateApp(context.Background(), input); err == nil {
			t.Errorf("invalid app case %d accepted", i)
		}
	}
}

func TestAppOptionalHashAndSensitiveResponseProjection(t *testing.T) {
	base := appJSON(t, appFixture())
	app, err := decodeApp([]byte(base))
	if err != nil || app.SHA256 != nil || app.FileSize != nil {
		t.Fatal("missing metadata must remain absent, never inferred", err)
	}
	var fields map[string]any
	_ = json.Unmarshal([]byte(base), &fields)
	fields["sha256"], fields["file_size"], fields["file_url"] = strings.Repeat("A", 64), 123, "https://synthetic.invalid/?signed=synthetic-secret"
	data, _ := json.Marshal(fields)
	app, err = decodeApp(data)
	if err != nil || *app.SHA256 != strings.Repeat("a", 64) || *app.FileSize != 123 {
		t.Fatal("documented metadata not decoded", err)
	}
	encoded, _ := json.Marshal(app)
	if strings.Contains(string(encoded), "signed") || strings.Contains(string(encoded), "file_url") {
		t.Fatal("signed download URL entered app model")
	}
	for _, key := range []string{"name", "file_key", "install_type", "active"} {
		copy := map[string]any{}
		_ = json.Unmarshal([]byte(base), &copy)
		delete(copy, key)
		invalid, _ := json.Marshal(copy)
		partial, err := decodeApp(invalid)
		if err == nil || partial.ID != testID {
			t.Fatal("invalid response must preserve returned ID for recovery")
		}
	}
	fields["sha256"] = "synthetic-secret"
	invalid, _ := json.Marshal(fields)
	if _, err := decodeApp(invalid); err == nil || strings.Contains(err.Error(), "synthetic-secret") {
		t.Fatal("malformed hash accepted or leaked")
	}
}

func TestAppWriteErrorsNeverReplayOrExposeBodies(t *testing.T) {
	for _, status := range []int{401, 403, 404, 429, 503} {
		calls := 0
		c := testClient(t, func(*http.Request) (*http.Response, error) {
			calls++
			return response(status, testToken+" synthetic-presigned-body"), nil
		})
		_, err := c.CreateApp(context.Background(), appFixture())
		if err == nil || calls != 1 || strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), "presigned") {
			t.Fatal("ambiguous app write replayed or leaked response")
		}
	}
	c := testClient(t, func(*http.Request) (*http.Response, error) { return response(404, ""), nil })
	if err := c.DeleteApp(context.Background(), testID); err != nil {
		t.Fatal("absent app deletion must succeed")
	}
}
