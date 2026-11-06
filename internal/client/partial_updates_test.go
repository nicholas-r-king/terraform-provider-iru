// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestScriptMetadataPatchOmitsUnchangedOptionalFields(t *testing.T) {
	prior := fixtureInput()
	input := prior
	input.Name = "Synthetic renamed"
	c := testClient(t, func(req *http.Request) (*http.Response, error) {
		var fields map[string]any
		if json.NewDecoder(req.Body).Decode(&fields) != nil || len(fields) != 1 || fields["name"] != input.Name {
			t.Fatal("script metadata PATCH resent unchanged fields")
		}
		return response(200, fixtureJSON(t, input)), nil
	})
	if _, err := c.UpdateScript(context.Background(), testID, input, prior); err != nil {
		t.Fatal(err)
	}
}
func TestScriptChangedRemediationClearRefusesBeforeTransport(t *testing.T) {
	prior := fixtureInput()
	prior.RemediationScript = "#!/bin/sh\nexit 0\n"
	input := prior
	input.RemediationScript = ""
	calls := 0
	c := testClient(t, func(req *http.Request) (*http.Response, error) {
		calls++
		return response(200, fixtureJSON(t, input)), nil
	})
	if _, err := c.UpdateScript(context.Background(), testID, input, prior); err == nil || calls != 0 {
		t.Fatal("unsupported remediation clear reached transport")
	}
}
func TestAppCreateOmitsAbsentOptionalContent(t *testing.T) {
	input := appFixture()
	c := testClient(t, func(req *http.Request) (*http.Response, error) {
		if req.ParseForm() != nil {
			t.Fatal("invalid form")
		}
		for _, field := range []string{"audit_script", "preinstall_script", "postinstall_script", "unzip_location"} {
			if req.Form.Has(field) {
				t.Fatal("absent optional field sent on create")
			}
		}
		if req.Form.Get("active") != "false" || req.Form.Get("restart") != "false" {
			t.Fatal("explicit false lost")
		}
		return response(201, appJSON(t, input)), nil
	})
	if _, err := c.CreateApp(context.Background(), input); err != nil {
		t.Fatal(err)
	}
}
func TestAppMetadataPatchAndContentClearing(t *testing.T) {
	prior := appFixture()
	input := prior
	input.Name = "Synthetic renamed"
	clear := false
	c := testClient(t, func(req *http.Request) (*http.Response, error) {
		if req.ParseForm() != nil {
			t.Fatal("invalid form")
		}
		if !clear {
			if len(req.Form) != 1 || req.Form.Get("name") != input.Name {
				t.Fatal("app metadata PATCH resent unchanged fields")
			}
		} else {
			if !req.Form.Has("preinstall_script") || req.Form.Get("preinstall_script") != "" || len(req.Form) != 2 {
				t.Fatal("app script clearing lost explicit empty value")
			}
		}
		return response(200, appJSON(t, input)), nil
	})
	if _, err := c.UpdateApp(context.Background(), testID, input, false, prior); err != nil {
		t.Fatal(err)
	}
	prior = input
	prior.PreinstallScript = "#!/bin/sh\nexit 0\n"
	clear = true
	if _, err := c.UpdateApp(context.Background(), testID, input, false, prior); err != nil {
		t.Fatal(err)
	}
}
