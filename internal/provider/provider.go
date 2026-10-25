// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"net/http"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
)

var _ provider.Provider = &IruProvider{}

type IruProvider struct {
	version string
	// Test injection is available only through Go construction. Production New
	// leaves this nil; no provider configuration or runtime bypass is exposed.
	httpClient *http.Client
}
type providerModel struct {
	APIURL   types.String `tfsdk:"api_url"`
	APIToken types.String `tfsdk:"api_token"`
}

func New(version string) func() provider.Provider {
	return func() provider.Provider { return &IruProvider{version: version} }
}

func (p *IruProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "iru"
	resp.Version = p.version
}

func (p *IruProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Development provider for Iru Custom Scripts. Prefer IRU_API_URL and IRU_API_TOKEN environment variables.",
		Attributes: map[string]schema.Attribute{
			"api_url":   schema.StringAttribute{Optional: true, MarkdownDescription: "HTTPS tenant API origin; defaults to IRU_API_URL."},
			"api_token": schema.StringAttribute{Optional: true, Sensitive: true, MarkdownDescription: "Bearer API token; defaults to IRU_API_TOKEN. Prefer the environment and keep credentials out of configuration."},
		},
	}
}

func (p *IruProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if config.APIURL.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("api_url"), "Unknown Iru API URL", "api_url must be known when configuring the provider.")
	}
	if config.APIToken.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("api_token"), "Unknown Iru API token", "api_token must be known when configuring the provider.")
	}
	if resp.Diagnostics.HasError() {
		return
	}
	baseURL, token := os.Getenv("IRU_API_URL"), os.Getenv("IRU_API_TOKEN")
	if !config.APIURL.IsNull() {
		baseURL = config.APIURL.ValueString()
	}
	if !config.APIToken.IsNull() {
		token = config.APIToken.ValueString()
	}
	c, err := client.New(baseURL, token, p.httpClient)
	if err != nil {
		resp.Diagnostics.AddError("Invalid Iru provider configuration", err.Error())
		return
	}
	resp.ResourceData = c
}

func (*IruProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{NewCustomScriptResource}
}

func (*IruProvider) DataSources(context.Context) []func() datasource.DataSource { return nil }
