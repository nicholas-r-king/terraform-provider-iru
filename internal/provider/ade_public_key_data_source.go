// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/nicholas-r-king/terraform-provider-iru/internal/client"
)

type ADEPublicKeyDataSource struct{ client *client.Client }

type adePublicKeyModel struct {
	ID           types.String `tfsdk:"id"`
	PublicKeyPEM types.String `tfsdk:"public_key_pem"`
	SHA256       types.String `tfsdk:"sha256"`
}

var (
	_ datasource.DataSource              = &ADEPublicKeyDataSource{}
	_ datasource.DataSourceWithConfigure = &ADEPublicKeyDataSource{}
)

func NewADEPublicKeyDataSource() datasource.DataSource { return &ADEPublicKeyDataSource{} }

func (*ADEPublicKeyDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ade_public_key"
}

func (*ADEPublicKeyDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Reads the tenant's public ADE bootstrap PEM for creating an MDM server connection in Apple Business Manager. Returns public material only and makes no mutations. Parsing checks public certificate/key syntax, not certificate trust, validity dates, or ABM account suitability.", Attributes: map[string]schema.Attribute{
		"id":             schema.StringAttribute{Computed: true, MarkdownDescription: "Logical ID equal to sha256 of the exact API response bytes. Changes when the response changes."},
		"public_key_pem": schema.StringAttribute{Computed: true, MarkdownDescription: "Exact public certificate/key PEM response, including whitespace. Stored in Terraform state. Private key blocks, malformed PEM/DER, multiple blocks, PEM headers, and other content are rejected."},
		"sha256":         schema.StringAttribute{Computed: true, MarkdownDescription: "Lowercase SHA256 digest of the exact API response bytes, including formatting. This is not a normalized public-key fingerprint or a trust assertion."},
	}}
}

func (d *ADEPublicKeyDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok || c == nil {
		resp.Diagnostics.AddError("Unexpected Iru client type", "Could not configure the ADE public key lookup.")
		return
	}
	d.client = c
}

func (d *ADEPublicKeyDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("ADE public key lookup unavailable", "Configure the provider before reading the public ADE bootstrap PEM.")
		return
	}
	value, err := d.client.GetADEPublicKey(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Iru ADE public key", err.Error())
		return
	}
	data := adePublicKeyModel{ID: types.StringValue(value.SHA256), PublicKeyPEM: types.StringValue(value.PEM), SHA256: types.StringValue(value.SHA256)}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
