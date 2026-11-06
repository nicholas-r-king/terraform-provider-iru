// SPDX-License-Identifier: MPL-2.0
package client

import (
	"strings"
	"testing"
)

func TestInHouseConfigurationDoesNotRelaxManagedDecoder(t *testing.T) {
	body := `{"id":"` + testID + `","name":"Synthetic","file_key":"synthetic/app.ipa","active":false,"runs_on_iphone":false,"runs_on_ipad":false,"runs_on_tv":false,"sha256":"` + strings.Repeat("AB", 32) + `","file_size":0}`
	value, err := decodeInHouseAppConfiguration([]byte(body), testID)
	if err != nil || value.SHA256 == nil || *value.SHA256 != strings.Repeat("AB", 32) || value.FileSize == nil || *value.FileSize != 0 {
		t.Fatal("observation imposed write defaults or normalized metadata")
	}
	if _, err := decodeInHouseApp([]byte(body), testID); err == nil {
		t.Fatal("configuration lookup weakened managed platform validation")
	}
	body = strings.Replace(body, `"runs_on_tv":false`, `"runs_on_tv":true`, 1)
	value, err = decodeInHouseApp([]byte(body), testID)
	if err != nil || value.SHA256 == nil || *value.SHA256 != strings.Repeat("ab", 32) {
		t.Fatal("managed decoder compatibility changed")
	}
}
