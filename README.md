<!-- SPDX-License-Identifier: MPL-2.0 -->

# Terraform provider for Iru

Unofficial Iru endpoint-management provider, using the Terraform Plugin Framework.
Releases are experimental prereleases. Check the
[Terraform Registry](https://registry.terraform.io/providers/nicholas-r-king/iru)
for available versions and pin the selected version explicitly.

The provider manages Custom Scripts, Profiles, Custom Apps, In-house Apps, Tags,
Device Notes, Blueprint metadata, ADE integrations and selected device/enrollment
settings. Read-only data sources expose configuration, identity catalogs and
Library Item status snapshots. See the [resource reference](docs/index.md) and
[API coverage](API_COVERAGE.md).

Current Assignment Map graphs/placement, routing rules, built-in Library Item
parameters and category CRUD are not implemented. The classic assignment resource
is deprecated and refuses current maps. Blueprint/ADE deletion is blocked because
it can affect assigned devices; use `lifecycle { prevent_destroy = true }` for
retention, including tainted replacements.

## Configuration

Supply `IRU_API_URL` and `IRU_API_TOKEN` through your secure environment:

```hcl
terraform {
  required_providers {
    iru = {
      source  = "nicholas-r-king/iru"
      version = "= 0.2.0-alpha.1"
    }
  }
}

provider "iru" {}

resource "iru_custom_script" "example" {
  name                = "Example audit"
  script              = "#!/bin/sh\nexit 0\n"
  execution_frequency = "once"
  active              = false
}
```

Use a [development override](CONTRIBUTING.md#local-build) for source changes that
have not been released. Examples are under [examples/](examples/).

Import reads existing objects. Match their configuration and inspect the first
plan: omitted defaults can change imported settings. Script/XML content, installer
keys and enrollment codes remain in state despite display redaction. A failed
write may have committed; reconcile before retrying. Library Item deletion can
affect assignments and does not prove installer-file deletion.

## Development and validation

Run `go run ./tools verify -check-docs` with Go and Terraform installed. Tests use
synthetic APIs. [Validation status](VALIDATION_STATUS.md) distinguishes automated
coverage from selected live tests and remaining gaps; complete live acceptance is
still open. Contributor and release instructions are in
[CONTRIBUTING.md](CONTRIBUTING.md) and [RELEASING.md](RELEASING.md).

## License

[MPL-2.0](LICENSE). Dependencies retain their own licenses; release packages
include the required notices and source-availability material.
