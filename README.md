<!-- SPDX-License-Identifier: MPL-2.0 -->

# Terraform provider for Iru

An unofficial development provider for Iru device management, built with the
HashiCorp Terraform Plugin Framework. Resources include `iru_custom_script` and
`iru_custom_app` (using an uploaded key or local installer), and `iru_custom_profile`.
It also manages `iru_blueprint` metadata/enrollment settings, `iru_tag`,
`iru_in_house_app` (using a validated key or local IPA), `iru_ade_integration`
(including Apple token renewal), and `iru_device_note`.
`iru_blueprint_library_item_assignment` manages one classic Blueprint root
membership; it is locally tested and still requires live acceptance. Assignment
Map nodes/parameters are not managed. The `iru_inventory` data source lists UUID/name metadata for eight
configuration object families, including Self Service categories.
See [API coverage](API_COVERAGE.md) for the remaining API gaps.

`iru_blueprint_routing` also manages or reads tenant-wide manual enrollment
settings. This singleton has GET/PATCH only: matching adoption/import is
read-only, and destroy leaves Iru settings intact while releasing management.

`iru_device_settings` and `iru_ade_device_settings` adopt existing device records
and manage only configured assignment metadata through GET/PATCH. Destroy retains
the devices and their settings. Exact-UUID `iru_device`, `iru_ade_device`, and
`iru_user` lookups support selecting existing assignments; they perform no writes.

This remains experimental. A [published preview](https://registry.terraform.io/providers/nicholas-r-king/iru/0.1.0-alpha.1)
exists, but it predates the draft safety/compatibility fixes and new assignment
resource. Selected resource lifecycles and metadata reads have live evidence;
full field, assignment, enrollment and device-delivery acceptance remains
incomplete. See [API coverage](API_COVERAGE.md), inspect the exact commit's CI and
plan, and do not treat a mock or schema pass as production readiness.

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
[Custom App reference](docs/resources/custom_app.md),
[Custom Profile reference](docs/resources/custom_profile.md), and
[complete local example](examples/custom_script/main.tf). Local use requires a
Terraform CLI development override pointing to the built provider; `terraform
init` cannot download this unpublished provider.

Script contents are sensitive in displays but remain in Terraform state. Import
reads the existing item; a no-op plan requires matching configuration. Omitted
static defaults can plan changes to imported settings. Destroy permanently deletes
the Library Item and can affect assignments managed elsewhere. After an ambiguous
creation failure, inspect/import the item before retrying.

Read-only lookups use exact UUIDs:
[Blueprint](docs/data-sources/blueprint.md) returns ID/name/type, and
[Library Item](docs/data-sources/library_item.md) returns ID/name membership
metadata within an existing Blueprint. They do not expose enrollment codes or
device inventory, or manage assignment-node placement.

## Development

Linux and macOS are the primary targets. Go 1.25 or later is required by the
pinned Framework. From the repository root, with Go and Terraform on PATH, run
`go run ./tools verify -check-docs`. The same Go tooling works for optional Windows
development; no PowerShell is required. See [CONTRIBUTING.md](CONTRIBUTING.md).
CI uses Terraform 1.17.0 with Go 1.25.0 and 1.27.3. Linux/macOS amd64 and arm64
have native test jobs. No provider repository tests contact a live Iru tenant.
New resources, API fields, and acceptance tests require a verified contract.
Public Registry release preparation and its remaining owner actions are described
in [RELEASING.md](RELEASING.md); the manual workflow creates experimental drafts.

## License

Original source is licensed under [MPL-2.0](LICENSE). No third-party source is
vendored or copied into this repository. Dependencies retain their own licenses.
Future binary distributions must include the applicable generated license,
copyright, NOTICE, and MPL source-availability material for the components shipped.
