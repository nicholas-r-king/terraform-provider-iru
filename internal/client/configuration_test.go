// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

const blueprintFixture = `{"id":"11111111-2222-4333-8444-555555555555","name":"Synthetic blueprint","type":"map","description":"","color":"aqua-800","icon":"ss-files","enrollment_code":{"code":"000123","is_active":false}}`

func TestBlueprintCRUD(t *testing.T) {
	methods := []string{}
	c := testClient(t, func(req *http.Request) (*http.Response, error) {
		methods = append(methods, req.Method)
		if req.Method == "POST" || req.Method == "PATCH" {
			if req.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || req.ParseForm() != nil {
				t.Fatal("Blueprint must use documented form encoding")
			}
			if req.Form.Get("name") != "Synthetic blueprint" || req.Form.Get("enrollment_code.is_active") != "false" || req.Form.Get("enrollment_code.code") != "000123" {
				t.Fatal("managed Blueprint fields missing")
			}
			if req.Method == "POST" && req.Form.Get("type") != "map" {
				t.Fatal("create type missing")
			}
			if req.Method == "PATCH" && req.Form.Has("type") {
				t.Fatal("update must not send create-only type")
			}
		}
		if req.Method == "DELETE" {
			return response(204, ""), nil
		}
		if req.Method == "PATCH" {
			return response(200, `{"id":"`+testID+`"}`), nil
		}
		status := 200
		if req.Method == "POST" {
			status = 201
		}
		return response(status, blueprintFixture), nil
	})
	code := "000123"
	input := BlueprintInput{Name: "Synthetic blueprint", Type: "map", EnrollmentCode: &code}
	ctx := context.Background()
	if value, err := c.CreateBlueprint(ctx, input); err != nil || value.ID != testID {
		t.Fatalf("create: %v", err)
	}
	if _, err := c.GetManagedBlueprint(ctx, strings.ToUpper(testID)); err != nil {
		t.Fatal(err)
	}
	if value, err := c.UpdateBlueprint(ctx, testID, input); err != nil || value.Type != "map" {
		t.Fatalf("update/read: %v", err)
	}
	if err := c.DeleteBlueprint(ctx, testID); err != nil {
		t.Fatal(err)
	}
	if strings.Join(methods, ",") != "POST,GET,PATCH,GET,DELETE" {
		t.Fatal("unexpected requests")
	}
}

func TestBlueprintReadAndInputFailures(t *testing.T) {
	for _, body := range []string{`{}`, strings.Replace(blueprintFixture, `"is_active":false`, `"is_active":null`, 1), strings.Replace(blueprintFixture, `"type":"map",`, "", 1), strings.Replace(blueprintFixture, testID, testCategory, 1)} {
		c := testClient(t, func(*http.Request) (*http.Response, error) { return response(200, body), nil })
		if _, err := c.GetManagedBlueprint(context.Background(), testID); err == nil {
			t.Fatal("invalid managed Blueprint accepted")
		}
	}
	for _, input := range []BlueprintInput{{Name: " ", Type: "map"}, {Name: "Synthetic", Type: "unknown"}, {Name: "Synthetic", Type: "map", Color: "blue"}, {Name: "Synthetic", Type: "classic", SourceType: "blueprint"}, {Name: "Synthetic", Type: "classic", SourceType: "template", SourceID: "../1"}} {
		if input.Validate() == nil {
			t.Fatal("unsafe Blueprint input accepted")
		}
	}
}

func TestBlueprintClassicCreationRefusedBeforeTransport(t *testing.T) {
	methods := []string{}
	c := testClient(t, func(req *http.Request) (*http.Response, error) {
		methods = append(methods, req.Method)
		if req.Method == http.MethodPatch {
			if req.ParseForm() != nil || req.Form.Has("type") {
				t.Fatal("legacy metadata update attempted a type change")
			}
		}
		return response(http.StatusOK, strings.Replace(blueprintFixture, `"type":"map"`, `"type":"classic"`, 1)), nil
	})
	for _, source := range []BlueprintInput{
		{Name: "Synthetic legacy", Type: "classic"},
		{Name: "Synthetic legacy", Type: "classic", SourceType: "template", SourceID: "1"},
		{Name: "Synthetic legacy", Type: "classic", SourceType: "blueprint", SourceID: testCategory},
	} {
		if _, err := c.CreateBlueprint(context.Background(), source); err == nil || !strings.Contains(err.Error(), "classic Blueprint creation is unsupported") {
			t.Fatal("obsolete classic create/clone was not clearly refused")
		}
	}
	if len(methods) != 0 {
		t.Fatal("classic creation reached transport")
	}
	// Retain a legacy read/update path for existing state; this synthetic test
	// does not assert that the current service still exposes classic objects.
	value, err := c.UpdateBlueprint(context.Background(), testID, BlueprintInput{Name: "Synthetic legacy", Type: "classic"})
	if err != nil || value.Type != "classic" || strings.Join(methods, ",") != "PATCH,GET" {
		t.Fatal("legacy metadata compatibility was lost", err)
	}
}

func TestBlueprintCreateMismatchRetainsIdentityWithoutReplay(t *testing.T) {
	for _, body := range []string{
		strings.Replace(blueprintFixture, `"type":"map"`, `"type":"classic"`, 1),
		strings.Replace(blueprintFixture, `"is_active":false`, `"is_active":true`, 1),
	} {
		calls := 0
		c := testClient(t, func(req *http.Request) (*http.Response, error) {
			calls++
			if req.Method != http.MethodPost {
				t.Fatal("unexpected create verification request")
			}
			return response(http.StatusCreated, body), nil
		})
		value, err := c.CreateBlueprint(context.Background(), BlueprintInput{Name: "Synthetic map", Type: "map"})
		if err == nil || value.ID != testID || calls != 1 {
			t.Fatal("mismatched creation type/enrollment lost identity, succeeded or replayed")
		}
	}
}

func TestTagCRUDAndPagination(t *testing.T) {
	ctx := context.Background()
	methods := []string{}
	c := testClient(t, func(req *http.Request) (*http.Response, error) {
		methods = append(methods, req.Method)
		if req.Method == "GET" {
			if req.URL.Path != tagsPath {
				t.Fatal("invented tag detail endpoint")
			}
			if req.URL.RawQuery == "" {
				return response(200, `{"next":"?offset=1","results":[]}`), nil
			}
			return response(200, `{"next":null,"results":[{"id":"`+testID+`","name":"Synthetic tag"}]}`), nil
		}
		if req.Method == "DELETE" {
			return response(204, ""), nil
		}
		var fields map[string]string
		if json.NewDecoder(req.Body).Decode(&fields) != nil || len(fields) != 1 || fields["name"] != "Synthetic tag" {
			t.Fatal("wrong Tag payload")
		}
		status := 200
		if req.Method == "POST" {
			status = 201
		}
		return response(status, `{"id":"`+testID+`","name":"Synthetic tag"}`), nil
	})
	if _, err := c.CreateTag(ctx, "Synthetic tag"); err != nil {
		t.Fatal(err)
	}
	if value, err := c.GetTag(ctx, testID); err != nil || value.Name != "Synthetic tag" {
		t.Fatalf("lookup: %v", err)
	}
	if _, err := c.UpdateTag(ctx, testID, "Synthetic tag"); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteTag(ctx, testID); err != nil {
		t.Fatal(err)
	}
	if strings.Join(methods, ",") != "POST,GET,GET,PATCH,DELETE" {
		t.Fatal("unexpected Tag requests")
	}
}

func TestTagUnsafeAndIncompleteListings(t *testing.T) {
	for _, body := range []string{`{}`, `{"next":"https://evil.example/api/v1/tags?offset=1","results":[]}`, `{"next":"/api/v1/users?offset=1","results":[]}`, `{"next":"?offset=1","results":[]}`, `{"next":null,"results":[{"id":"` + testID + `","name":"a"},{"id":"` + testID + `","name":"b"}]}`} {
		calls := 0
		c := testClient(t, func(*http.Request) (*http.Response, error) { calls++; return response(200, body), nil })
		if _, err := c.GetTag(context.Background(), testID); err == nil {
			t.Fatal("unsafe/incomplete listing accepted")
		}
		if calls > 2 {
			t.Fatal("unbounded listing")
		}
	}
	c := testClient(t, func(*http.Request) (*http.Response, error) { return response(200, `{"next":null,"results":[]}`), nil })
	if _, err := c.GetTag(context.Background(), testID); !IsNotFound(err) {
		t.Fatal("missing Tag must signal disappearance")
	}
}

func TestTagAbsenceRequiresCompletedPagesNotCollection404(t *testing.T) {
	for _, laterPage := range []bool{false, true} {
		calls := 0
		c := testClient(t, func(req *http.Request) (*http.Response, error) {
			calls++
			if req.Method != http.MethodGet {
				t.Fatal("Tag absence lookup wrote to API")
			}
			if laterPage && calls == 1 {
				return response(200, `{"results":[{"id":"`+testID+`","name":"Synthetic"}],"next":"?offset=1"}`), nil
			}
			return response(404, `{"private":"synthetic-private-response"}`), nil
		})
		_, err := c.GetTag(context.Background(), testID)
		if err == nil || IsTagAbsent(err) || strings.Contains(err.Error(), "synthetic-private") {
			t.Fatal("failed collection/page was interpreted as absent Tag")
		}
	}
	c := testClient(t, func(*http.Request) (*http.Response, error) { return response(200, `{"results":[],"next":null}`), nil })
	if _, err := c.GetTag(context.Background(), testID); !IsTagAbsent(err) || !IsNotFound(err) {
		t.Fatal("completed missing lookup did not prove absence")
	}
}

func TestConfigurationWritesNeverReplayAndRedact(t *testing.T) {
	for _, status := range []int{400, 403, 429, 503} {
		calls := 0
		c := testClient(t, func(*http.Request) (*http.Response, error) {
			calls++
			return response(status, `{"name":["secret-message"]}`), nil
		})
		_, err := c.CreateTag(context.Background(), "Synthetic")
		if err == nil || calls != 1 || strings.Contains(err.Error(), "secret-message") {
			t.Fatalf("unsafe write handling at %d", status)
		}
	}
}

func TestCreateScriptNonemptyRemediation(t *testing.T) {
	input := fixtureInput()
	input.RemediationScript = "#!/bin/sh\nexit 0\n"
	c := testClient(t, func(req *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(req.Body)
		var payload map[string]any
		if json.Unmarshal(body, &payload) != nil || payload["remediation_script"] != input.RemediationScript {
			t.Fatal("create lost supplied remediation")
		}
		return response(201, fixtureJSON(t, input)), nil
	})
	if _, err := c.CreateScript(context.Background(), input); err != nil {
		t.Fatal(fmt.Sprint(err))
	}
}
