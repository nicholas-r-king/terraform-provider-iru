// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func inHouseFixture(t *testing.T, input InHouseAppInput) string {
	t.Helper()
	bytes, err := json.Marshal(InHouseApp{ID: testID, InHouseAppInput: input})
	if err != nil {
		t.Fatal(err)
	}
	return string(bytes)
}
func TestInHouseAppWireAndInstallerReplacement(t *testing.T) {
	input := InHouseAppInput{Name: "Synthetic IPA", FileKey: "tenants/synthetic/library/ipa_apps/example.ipa", RunsOnIphone: true}
	requests := 0
	replace := false
	c := testClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		if req.URL.Path != inHouseAppsPath && req.URL.Path != inHouseAppsPath+"/"+testID {
			t.Fatal("unexpected In-House App path")
		}
		if req.Method == "POST" || req.Method == "PATCH" {
			if req.Header.Get("Content-Type") != "application/json" {
				t.Fatal("IPA must use JSON, not custom-app form encoding")
			}
			var fields map[string]any
			if json.NewDecoder(req.Body).Decode(&fields) != nil {
				t.Fatal("invalid JSON")
			}
			if fields["active"] != false || fields["runs_on_iphone"] != true || fields["runs_on_ipad"] != false || fields["runs_on_tv"] != false {
				t.Fatal("lost explicit boolean values")
			}
			_, exists := fields["file_key"]
			if exists != (req.Method == "POST" || replace) {
				t.Fatal("unrequested installer replacement")
			}
		}
		if req.Method == "DELETE" {
			return response(204, ""), nil
		}
		status := 200
		if req.Method == "POST" {
			status = 201
		}
		return response(status, inHouseFixture(t, input)), nil
	})
	ctx := context.Background()
	if _, err := c.CreateInHouseApp(ctx, input); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetInHouseApp(ctx, testID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UpdateInHouseApp(ctx, testID, input, false); err != nil {
		t.Fatal(err)
	}
	replace = true
	if _, err := c.UpdateInHouseApp(ctx, testID, input, true); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteInHouseApp(ctx, testID); err != nil {
		t.Fatal(err)
	}
	if requests != 5 {
		t.Fatal("unexpected request count")
	}
}

const adeFixture = `{"id":"11111111-2222-4333-8444-555555555555","blueprint":{"id":"aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"},"defaults":{"phone":"1234567890","email":"synthetic@example.invalid"},"server_name":"Synthetic Apple Server","access_token_expiry":"2027-10-29T00:00:00Z"}`

func TestADEWireIncludesBinaryMultipartAndRenew(t *testing.T) {
	input := ADEInput{BlueprintID: testCategory, Phone: "1234567890", Email: "synthetic@example.invalid"}
	token := []byte{0, 1, 2, 250, 255}
	methods := []string{}
	c := testClient(t, func(req *http.Request) (*http.Response, error) {
		methods = append(methods, req.Method+" "+req.URL.Path)
		if req.Method == "POST" {
			if req.ParseMultipartForm(1024*1024) != nil {
				t.Fatal("ADE POST must use multipart")
			}
			defer req.MultipartForm.RemoveAll()
			file, header, err := req.FormFile("file")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			got, _ := io.ReadAll(file)
			if string(got) != string(token) || header.Filename != "server-token.p7m" || req.FormValue("blueprint_id") != testCategory || req.FormValue("email") != input.Email {
				t.Fatal("wrong ADE token or metadata")
			}
		}
		if req.Method == "PATCH" {
			var body ADEInput
			if json.NewDecoder(req.Body).Decode(&body) != nil || body != input {
				t.Fatal("wrong ADE PATCH payload")
			}
		}
		if req.Method == "DELETE" {
			return response(204, ""), nil
		}
		return response(200, adeFixture), nil
	})
	ctx := context.Background()
	if _, err := c.CreateADEIntegration(ctx, input, token); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetADEIntegration(ctx, testID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UpdateADEIntegration(ctx, testID, input); err != nil {
		t.Fatal(err)
	}
	if _, err := c.RenewADEIntegration(ctx, testID, input, token); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteADEIntegration(ctx, testID); err != nil {
		t.Fatal(err)
	}
	want := "POST " + adePath + "/,GET " + adePath + "/" + testID + ",PATCH " + adePath + "/" + testID + ",POST " + adePath + "/" + testID + "/renew,DELETE " + adePath + "/" + testID
	if strings.Join(methods, ",") != want {
		t.Fatal("incorrect ADE endpoints or repeated writes")
	}
}

func TestDeviceNoteCRUDUsesDocumentedStatusAndIdentity(t *testing.T) {
	content := "<p>Synthetic note</p>"
	methods := []string{}
	c := testClient(t, func(req *http.Request) (*http.Response, error) {
		methods = append(methods, req.Method)
		path := "/api/v1/devices/" + testCategory + "/notes"
		if req.Method != "POST" {
			path += "/" + testID
		}
		if req.URL.Path != path {
			t.Fatal("wrong device or note endpoint")
		}
		if req.Method == "POST" || req.Method == "PATCH" {
			var fields map[string]string
			if json.NewDecoder(req.Body).Decode(&fields) != nil || fields["content"] != content || len(fields) != 1 {
				t.Fatal("invalid Note content")
			}
		}
		status := 200
		if req.Method == "POST" {
			status = 201
		}
		return response(status, `{"note_id":"`+testID+`","content":"`+content+`"}`), nil
	})
	ctx := context.Background()
	if _, err := c.CreateDeviceNote(ctx, testCategory, content); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetDeviceNote(ctx, testCategory, testID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UpdateDeviceNote(ctx, testCategory, testID, content); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteDeviceNote(ctx, testCategory, testID); err != nil {
		t.Fatal(err)
	}
	if strings.Join(methods, ",") != "POST,GET,PATCH,DELETE" {
		t.Fatal("wrong Note lifecycle")
	}
}

func TestAdditionalObjectFailureContracts(t *testing.T) {
	ctx := context.Background()
	for _, status := range []int{400, 401, 403, 429, 503} {
		calls := 0
		c := testClient(t, func(*http.Request) (*http.Response, error) {
			calls++
			return response(status, `{"file":["synthetic-private-token"],"content":["synthetic-private-note"]}`), nil
		})
		_, err := c.CreateADEIntegration(ctx, ADEInput{BlueprintID: testCategory, Phone: "123", Email: "synthetic@example.invalid"}, []byte("synthetic-private-token"))
		if err == nil || calls != 1 || strings.Contains(err.Error(), "synthetic-private") {
			t.Fatal("ADE write retried or exposed response")
		}
		calls = 0
		_, err = c.CreateDeviceNote(ctx, testCategory, "Synthetic")
		if err == nil || calls != 1 || strings.Contains(err.Error(), "synthetic-private") {
			t.Fatal("Note write retried or exposed response")
		}
		calls = 0
		_, err = c.CreateInHouseApp(ctx, InHouseAppInput{Name: "Synthetic", FileKey: "synthetic.ipa", RunsOnIpad: true})
		if err == nil || calls != 1 || strings.Contains(err.Error(), "synthetic-private") {
			t.Fatal("IPA write retried or exposed response")
		}
	}
	c := testClient(t, func(*http.Request) (*http.Response, error) { return response(404, ""), nil })
	if c.DeleteInHouseApp(ctx, testID) != nil || c.DeleteADEIntegration(ctx, testID) != nil || c.DeleteDeviceNote(ctx, testCategory, testID) != nil {
		t.Fatal("idempotent delete should accept disappearance")
	}
	if _, err := decodeInHouseApp([]byte(`{"id":"`+testID+`"}`), ""); err == nil {
		t.Fatal("missing IPA fields accepted")
	}
	if _, err := decodeADE([]byte(strings.Replace(adeFixture, `"phone":"1234567890"`, `"phone":null`, 1)), ""); err == nil {
		t.Fatal("missing ADE contact accepted")
	}
	if _, err := decodeDeviceNote([]byte(`{"note_id":"`+testID+`","content":null}`), ""); err == nil {
		t.Fatal("null Note content accepted")
	}
	if _, err := decodeDeviceNote([]byte(`{"note_id":"`+testID+`","content":"synthetic"}`), testCategory); err == nil {
		t.Fatal("mismatched Note ID accepted")
	}
}
