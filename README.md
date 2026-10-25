<!-- SPDX-License-Identifier: MPL-2.0 -->

# Terraform provider for Iru

An unofficial development provider for Iru device management, built with the
HashiCorp Terraform Plugin Framework. Resources include `iru_custom_script` and
`iru_custom_app` (using an existing uploaded installer key).
It does not manage Blueprint/Assignment Map or device assignments.

This is a review milestone, not a production-ready release. Synthetic unit and
Terraform CLI tests pass; real Iru compatibility has not been acceptance-tested.
Vet remains incomplete in the current Windows validation environment. No Registry
release or installable published version exists yet.

## Configuration and examples

Load `IRU_API_URL` and `IRU_API_TOKEN` through your existing secure environment.
Do not put credentials in committed HCL, scripts, tfvars, issues, or logs.

```hcl
provider "iru" {}

resource "iru_custom_script" "example" {
  name                = "Synthetic audit example"
  script              = "#!/bin/sh\nexit 0\n"
  execution_frequency = "once"
  active              = false
}
```

See the [provider reference](docs/index.md),
[Custom Script reference](docs/resources/custom_script.md),
[Custom App reference](docs/resources/custom_app.md), and
[complete local example](examples/custom_script/main.tf). Local use requires a
Terraform CLI development override pointing to the built provider; `terraform
init` cannot download this unpublished provider.

Script contents are sensitive in displays but remain in Terraform state. Import
reads the existing item; a no-op plan requires matching configuration. Omitted
static defaults can plan changes to imported settings. Destroy permanently deletes
the Library Item and can affect assignments managed elsewhere. After an ambiguous
creation failure, inspect/import the item before retrying.

## Development

Linux and macOS are the primary targets. Go 1.25 or later is required by the
pinned Framework. From the repository root, with Go and Terraform on PATH, run
`go run ./tools verify -check-docs`. The same Go tooling works for optional Windows
development; no PowerShell is required. See [CONTRIBUTING.md](CONTRIBUTING.md).
Runtime validation so far used Go 1.27.3 and Terraform 1.17.0 on Windows amd64;
Linux/macOS native validation is still required. No tests contact a live Iru tenant.
New resources, API fields, and acceptance tests require a verified contract.

## License

Original source is licensed under [MPL-2.0](LICENSE). No third-party source is
vendored or copied into this repository. Dependencies retain their own licenses.
Future binary distributions must include the applicable generated license,
copyright, NOTICE, and MPL source-availability material for the components shipped.
