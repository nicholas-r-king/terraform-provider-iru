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

const syntheticXML = `<?xml version="1.0"?><plist version="1.0"><dict><key>PayloadDisplayName</key><string>Synthetic profile</string></dict></plist>`

func profileFixture() ProfileInput {
	return ProfileInput{Name: "Synthetic profile", Profile: syntheticXML, RunsOnMac: true}
}

func profileJSON(t *testing.T, input ProfileInput) string {
	t.Helper()
	data, err := json.Marshal(Profile{ID: testID, MDMIdentifier: "com.example.synthetic", ProfileInput: input})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestProfileWireContract(t *testing.T) {
	input := profileFixture()
	input.Profile = strings.Replace(syntheticXML, "Synthetic profile", "Synthetic\t profile\nvalue", 1)
	steps := []string{"POST", "GET", "PATCH", "PATCH", "DELETE"}
	index := 0
	c := testClient(t, func(req *http.Request) (*http.Response, error) {
		if index >= len(steps) || req.Method != steps[index] || !strings.HasPrefix(req.URL.Path, profilesPath) || req.Header.Get("Authorization") != "Bearer "+testToken {
			t.Fatal("unexpected profile request")
		}
		step := index
		index++
		if step == 0 || step == 3 {
			if req.ParseMultipartForm(responseLimit) != nil {
				t.Fatal("profile content must use multipart encoding")
			}
			defer req.MultipartForm.RemoveAll()
			file, header, err := req.FormFile("file")
			if err != nil {
				t.Fatal("missing profile file")
			}
			defer file.Close()
			content, _ := io.ReadAll(file)
			if string(content) != input.Profile || header.Filename != "terraform.mobileconfig" || req.FormValue("active") != "false" || req.FormValue("runs_on_mac") != "true" || req.FormValue("runs_on_vision") != "false" {
				t.Fatal("profile bytes or explicit target flags changed")
			}
		} else if step == 2 {
			if req.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || req.ParseForm() != nil || req.Form.Get("name") != input.Name {
				t.Fatal("metadata-only profile PATCH must omit the file")
			}
		}
		status := 200
		if step == 0 {
			status = 201
		} else if step == 4 {
			status = 204
		}
		return response(status, profileJSON(t, input)), nil
	})
	ctx := context.Background()
	if _, err := c.CreateProfile(ctx, input); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetProfile(ctx, strings.ToUpper(testID)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UpdateProfile(ctx, testID, input, false); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UpdateProfile(ctx, testID, input, true); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteProfile(ctx, testID); err != nil || index != len(steps) {
		t.Fatal("profile lifecycle incomplete", err)
	}
}

func TestProfileSyntaxAndPlatformValidation(t *testing.T) {
	for _, content := range []string{"", "synthetic-secret", "<plist>", "<other/>", "<plist/><plist/>", `<plist><string>&synthetic-secret;</string></plist>`} {
		input := profileFixture()
		input.Profile = content
		c := testClient(t, func(*http.Request) (*http.Response, error) { t.Fatal("invalid XML reached transport"); return nil, nil })
		_, err := c.CreateProfile(context.Background(), input)
		if err == nil || strings.Contains(err.Error(), "synthetic-secret") {
			t.Fatal("invalid XML accepted or exposed")
		}
	}
	input := profileFixture()
	input.RunsOnMac = false
	if input.Validate() == nil {
		t.Fatal("profile must choose target platforms")
	}
	input.RunsOnVision = true
	if input.Validate() != nil {
		t.Fatal("documented vendor vision target rejected")
	}
	xxe := `<!DOCTYPE plist [<!ENTITY synthetic SYSTEM "https://synthetic.invalid/never-fetch">]><plist><string>&synthetic;</string></plist>`
	if ValidateProfileXML(xxe) == nil {
		t.Fatal("external entities must not be expanded")
	}
}

func TestProfileResponseValidationAndWriteSafety(t *testing.T) {
	for _, value := range []json.RawMessage{nil, json.RawMessage("null")} {
		var fields map[string]json.RawMessage
		_ = json.Unmarshal([]byte(profileJSON(t, profileFixture())), &fields)
		if value == nil {
			delete(fields, "runs_on_vision")
		} else {
			fields["runs_on_vision"] = value
		}
		encoded, _ := json.Marshal(fields)
		if _, err := decodeProfile(encoded); err == nil {
			t.Fatal("missing/null Vision target must not become known false state")
		}
	}
	for _, status := range []int{400, 401, 403, 429, 503} {
		calls := 0
		c := testClient(t, func(*http.Request) (*http.Response, error) {
			calls++
			return response(status, testToken+" synthetic-profile-secret"), nil
		})
		_, err := c.CreateProfile(context.Background(), profileFixture())
		if err == nil || calls != 1 || strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), "profile-secret") {
			t.Fatal("profile write replayed or error leaked content")
		}
	}
	partial, err := decodeProfile([]byte(`{"id":"` + testID + `"}`))
	if err == nil || partial.ID != testID {
		t.Fatal("malformed profile response must retain known ID")
	}
	c := testClient(t, func(*http.Request) (*http.Response, error) { return response(404, ""), nil })
	if c.DeleteProfile(context.Background(), testID) != nil {
		t.Fatal("already absent profile deletion must succeed")
	}
	c = testClient(t, func(*http.Request) (*http.Response, error) {
		return response(200, strings.Replace(profileJSON(t, profileFixture()), testID, testCategory, 1)), nil
	})
	if _, err := c.GetProfile(context.Background(), testID); err == nil {
		t.Fatal("mismatched profile identity accepted")
	}
}
