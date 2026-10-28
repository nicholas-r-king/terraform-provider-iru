// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
)

func TestBlueprintLookupStateProjectsMetadata(t *testing.T) {
	ctx := context.Background()
	c, err := client.New("https://synthetic.api.iru.com", "synthetic-token", &http.Client{Transport: syntheticTransport(func(*http.Request) (*http.Response, error) {
		body := `{"id":"` + syntheticID + `","name":"Synthetic blueprint","type":"classic","enrollment_code":{"code":"synthetic-secret"},"computers_count":99}`
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	d := &BlueprintDataSource{client: c}
	var schemaResponse datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &schemaResponse)
	state := tfsdk.State{Schema: schemaResponse.Schema, Raw: tftypes.NewValue(schemaResponse.Schema.Type().TerraformType(ctx), nil)}
	config := blueprintLookupModel{BlueprintID: types.StringValue(strings.ToUpper(syntheticID)), ID: types.StringNull(), Name: types.StringNull(), Type: types.StringNull()}
	if diags := state.Set(ctx, &config); diags.HasError() {
		t.Fatal(diags)
	}
	resp := datasource.ReadResponse{State: state}
	d.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: state.Schema, Raw: state.Raw}}, &resp)
	var result blueprintLookupModel
	if diags := resp.State.Get(ctx, &result); diags.HasError() || resp.Diagnostics.HasError() || result.ID.ValueString() != syntheticID || result.BlueprintID.ValueString() != strings.ToUpper(syntheticID) {
		t.Fatal("lookup must preserve input spelling and return canonical identity", resp.Diagnostics)
	}
	if len(schemaResponse.Schema.Attributes) != 4 || schemaResponse.Schema.Attributes["enrollment_code"] != nil {
		t.Fatal("Blueprint schema exposes private response fields")
	}
}

func TestLibraryLookupSuccessAndFailurePreserveState(t *testing.T) {
	ctx := context.Background()
	status := 200
	c, err := client.New("https://synthetic.api.iru.com", "synthetic-token", &http.Client{Transport: syntheticTransport(func(*http.Request) (*http.Response, error) {
		body := `{"next":null,"results":[{"id":"` + syntheticID + `","name":"Synthetic item"}]}`
		if status != 200 {
			body = "synthetic-token sensitive response"
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	d := &LibraryItemDataSource{client: c}
	var schemaResponse datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &schemaResponse)
	state := tfsdk.State{Schema: schemaResponse.Schema, Raw: tftypes.NewValue(schemaResponse.Schema.Type().TerraformType(ctx), nil)}
	config := libraryItemLookupModel{BlueprintID: types.StringValue(strings.ToUpper(syntheticCategory)), LibraryItemID: types.StringValue(strings.ToUpper(syntheticID)), ID: types.StringNull(), Name: types.StringNull()}
	if diags := state.Set(ctx, &config); diags.HasError() {
		t.Fatal(diags)
	}
	req := datasource.ReadRequest{Config: tfsdk.Config{Schema: state.Schema, Raw: state.Raw}}
	resp := datasource.ReadResponse{State: state}
	d.Read(ctx, req, &resp)
	var result libraryItemLookupModel
	if diags := resp.State.Get(ctx, &result); diags.HasError() || resp.Diagnostics.HasError() || result.ID.ValueString() != syntheticID || result.LibraryItemID.ValueString() != strings.ToUpper(syntheticID) || result.BlueprintID.ValueString() != strings.ToUpper(syntheticCategory) {
		t.Fatal("lookup did not preserve selectors and project canonical metadata", resp.Diagnostics)
	}
	status = 401
	failed := datasource.ReadResponse{State: resp.State}
	d.Read(ctx, req, &failed)
	if !failed.Diagnostics.HasError() || !failed.State.Raw.Equal(resp.State.Raw) || strings.Contains(fmt.Sprint(failed.Diagnostics), "synthetic-token") {
		t.Fatal("failed lookup must preserve prior state and withhold response details")
	}
}

func TestLibraryLookupRejectsUnknownInputBeforeTransport(t *testing.T) {
	ctx := context.Background()
	c, err := client.New("https://synthetic.api.iru.com", "synthetic-token", &http.Client{Transport: syntheticTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("unknown lookup reached transport")
		return nil, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	d := &LibraryItemDataSource{client: c}
	var schemaResponse datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &schemaResponse)
	state := tfsdk.State{Schema: schemaResponse.Schema, Raw: tftypes.NewValue(schemaResponse.Schema.Type().TerraformType(ctx), nil)}
	config := libraryItemLookupModel{BlueprintID: types.StringUnknown(), LibraryItemID: types.StringValue(syntheticID), ID: types.StringNull(), Name: types.StringNull()}
	if diags := state.Set(ctx, &config); diags.HasError() {
		t.Fatal(diags)
	}
	resp := datasource.ReadResponse{State: state}
	d.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: state.Schema, Raw: state.Raw}}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("unknown lookup must defer without an HTTP request")
	}
}
