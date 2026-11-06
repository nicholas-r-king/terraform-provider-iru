// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"net/http"
	"testing"
)

func TestAppConfigurationInvalidSelectorNeverReachesTransport(t *testing.T) {
	c := testClient(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid configuration ID reached transport")
		return nil, nil
	})
	for _, id := range []string{"", "partial-name", "../custom-apps"} {
		if _, err := c.GetAppConfiguration(context.Background(), id); err == nil {
			t.Fatal("invalid selector accepted")
		}
	}
}
