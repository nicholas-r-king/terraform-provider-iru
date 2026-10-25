// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"strings"
	"testing"

	fwprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
)

func providerConfig(t *testing.T, model providerModel) tfsdk.Config {
	t.Helper()
	ctx := context.Background()
	var schemaResp fwprovider.SchemaResponse
	(&IruProvider{}).Schema(ctx, fwprovider.SchemaRequest{}, &schemaResp)
	state := tfsdk.State{Schema: schemaResp.Schema, Raw: tftypes.NewValue(schemaResp.Schema.Type().TerraformType(ctx), nil)}
	if diags := state.Set(ctx, &model); diags.HasError() {
		t.Fatal(diags)
	}
	return tfsdk.Config{Raw: state.Raw, Schema: schemaResp.Schema}
}

func TestProviderConfiguration(t *testing.T) {
	t.Setenv("IRU_API_URL", "https://synthetic.api.iru.com")
	t.Setenv("IRU_API_TOKEN", "synthetic-token")
	cases := []struct {
		name      string
		model     providerModel
		wantError bool
	}{
		{"environment", providerModel{types.StringNull(), types.StringNull()}, false},
		{"explicit override", providerModel{types.StringValue("https://another.api.eu.kandji.io"), types.StringValue("explicit-synthetic-token")}, false},
		{"unknown URL", providerModel{types.StringUnknown(), types.StringNull()}, true},
		{"unknown token", providerModel{types.StringNull(), types.StringUnknown()}, true},
		{"empty override", providerModel{types.StringNull(), types.StringValue("")}, true},
		{"unsafe URL", providerModel{types.StringValue("https://example.com"), types.StringNull()}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var resp fwprovider.ConfigureResponse
			(&IruProvider{}).Configure(context.Background(), fwprovider.ConfigureRequest{Config: providerConfig(t, tc.model)}, &resp)
			if resp.Diagnostics.HasError() != tc.wantError {
				t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
			}
			if !tc.wantError {
				if _, ok := resp.ResourceData.(*client.Client); !ok {
					t.Error("missing client")
				}
			}
			printed := fmt.Sprint(resp.Diagnostics)
			if strings.Contains(printed, "synthetic-token") || strings.Contains(printed, "explicit-synthetic-token") {
				t.Fatal("configuration diagnostics leaked token")
			}
		})
	}
	t.Setenv("IRU_API_URL", "")
	t.Setenv("IRU_API_TOKEN", "")
	var resp fwprovider.ConfigureResponse
	(&IruProvider{}).Configure(context.Background(), fwprovider.ConfigureRequest{Config: providerConfig(t, providerModel{types.StringNull(), types.StringNull()})}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Error("missing environment accepted")
	}
}

func TestProtocolSchemaAndSensitiveAttributes(t *testing.T) {
	ctx := context.Background()
	server := providerserver.NewProtocol6(New("test")())()
	resp, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range resp.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			t.Fatalf("schema error: %s", d.Detail)
		}
	}
	if len(resp.ResourceSchemas) != 1 || resp.ResourceSchemas["iru_custom_script"] == nil {
		t.Fatal("unexpected registered resources")
	}
	var schemaResp fwprovider.SchemaResponse
	(&IruProvider{}).Schema(ctx, fwprovider.SchemaRequest{}, &schemaResp)
	if !schemaResp.Schema.Attributes["api_token"].IsSensitive() {
		t.Error("token schema is not sensitive")
	}
	schema := scriptSchema(t)
	for _, name := range []string{"script", "remediation_script"} {
		if !schema.Attributes[name].IsSensitive() {
			t.Errorf("%s is not sensitive", name)
		}
	}
	if diags := schema.ValidateImplementation(ctx); diags.HasError() {
		t.Fatal(diags)
	}
}
