// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestDirectoryUserProjection(t *testing.T) {
	c := testClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != "GET" || req.URL.Path != "/api/v1/users/"+testID {
			t.Fatal("user lookup invented an endpoint or mutation")
		}
		return response(200, `{"id":"`+testID+`","name":"Synthetic Person","email":"synthetic@example.invalid","active":false,"archived":true,"department":"synthetic-unrelated-private-data","deprecated_user_id":"do-not-retain"}`), nil
	})
	user, err := c.GetUser(context.Background(), testID)
	if err != nil || user.Active == nil || *user.Active || user.Archived == nil || !*user.Archived {
		t.Fatalf("user lookup failed: %v", err)
	}
	data, _ := json.Marshal(user)
	if strings.Contains(string(data), "synthetic-unrelated") || strings.Contains(string(data), "do-not-retain") {
		t.Fatal("user projection retained unrelated fields")
	}
	if strings.Contains(fmt.Sprintf("%v %#v", user, user), "synthetic@example.invalid") {
		t.Fatal("user formatting exposed personal metadata")
	}
}
func TestDirectoryUserUnknownStatusIsNotInvented(t *testing.T) {
	c := testClient(t, func(*http.Request) (*http.Response, error) { return response(200, `{"id":"`+testID+`"}`), nil })
	user, err := c.GetUser(context.Background(), testID)
	if err != nil || user.Active != nil || user.Archived != nil || user.Email != nil {
		t.Fatal("omitted user fields became false or fabricated values")
	}
	c = testClient(t, func(*http.Request) (*http.Response, error) {
		return response(200, `{"id":"`+testCategory+`","email":"private-value"}`), nil
	})
	if _, err := c.GetUser(context.Background(), testID); err == nil || strings.Contains(err.Error(), "private-value") {
		t.Fatal("mismatched user identity accepted or exposed")
	}
}
