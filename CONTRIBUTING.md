<!-- SPDX-License-Identifier: MPL-2.0 -->

# Contributing

Keep changes limited to the documented Iru API. Include a regression for behavior
changes and explain API evidence, state impact, and test results in the PR.
Use synthetic fixture values; never record real tokens, tenant payloads, device
identifiers, or unredacted error bodies. Preserve upstream notices for any reused
source and identify its license before adding it.

## Manual checks

Linux and macOS are the primary contributor platforms. Install Go and Terraform
through your existing trusted workflow and run these commands from the repository
root. The contributor tool is written in Go; no PowerShell, Bash, Make or other
shell-specific runtime is required. Windows contributors can use the same commands.

```sh
go run ./tools verify -check-docs
```

The command checks formatting, module integrity, a CGO-disabled trimmed build,
unit/Framework protocol tests, loopback-only Terraform CLI tests, vet, example
formatting, and deterministic documentation. It uses Go/Terraform from PATH or
explicit `-go`/`-terraform` executable paths; it installs no system software and
does not change PATH. Go may fetch pinned modules into its normal module cache.
For a quick unit run, use `go test ./...`; the CLI test runs only when
`IRU_TEST_TERRAFORM` names a local executable. The contributor `verify` command
enables it automatically when Terraform is available. Without Terraform, plain
`verify` reports that CLI tests were not run; `-check-docs` requires Terraform.

The tool gives child processes an isolated environment: tenant credentials,
Terraform overrides/debug settings and cross-compilation targets are not inherited.
Only synthetic configuration is used. Build outputs stay in ignored `.cache/`.
If local controls block execution, do not disable them or route around the
restriction. Compile-only checks are not runtime test passes. Vet and runtime
tests remain required in CI; `-skip-vet-reason` records a local skip explicitly.

Generate or check the reference pages with the pinned official HashiCorp
tool, exported local schema and existing Terraform executable:

```sh
go run ./tools docs
go run ./tools docs -check
```

Commit generated pages with their templates/examples. Do not edit generated
output directly. Internal research and validation notes do not belong in public
provider documentation.

## Testing and compatibility

- Fast tests cover configuration, wire fields, redaction, error handling, retry
  bounds, and response validation. Framework tests cover import/read, defaults,
  unknown/null values, drift and no-op planning. CLI tests use only a fixed
  synthetic hostname/token and a loopback mock.
- A later, separately authorized isolated tenant test is required before claiming
  production compatibility. Mocks do not verify API normalization, permissions,
  propagation, remediation clearing, or Self Service transitions. No live-test
  switch is currently implemented.
- Keep stable Library Item IDs and preserve known plan values. Only the stable
  ID uses `UseStateForUnknown`. Schema version zero is intentional; introduce
  Framework state upgrades only for a real incompatible state change, with
  saved-state tests for each supported prior version directly to the current one.
- Treat 404 refresh as disappearance; auth and update errors retain state.
  Never replay ambiguous POSTs or invent an undocumented idempotency key.
  Library Item lookups must validate and bound pagination on the tenant origin.

## CI and release gates

The Tests workflow runs formatting, build, vet, unit/Framework and mocked
Terraform CLI lifecycle tests, example formatting and a generated-doc clean-diff
check. It covers native Linux/macOS amd64 and arm64, plus minimum Go 1.25.0 on
Linux amd64, using Terraform 1.17.0. Actions are pinned to immutable SHAs with
read-only repository permissions and no tenant secrets. There is no release or
live-test workflow in this repository.

Before a Registry release: choose a SemVer tag and changelog, verify the declared
Go/Terraform minimums, build and smoke-test each supported OS/architecture,
package applicable license/copyright/NOTICE material and MPL source locations,
and create correctly named ZIPs, protocol manifest, SHA256 sums and checksum
signature under the current [publishing requirements](https://developer.hashicorp.com/terraform/registry/providers/publishing).
Linux/macOS amd64 and arm64 are the primary target matrix; Windows is optional.
Cross-compilation does not establish runtime compatibility. Require passing
native CI results for the exact commit, including loopback mock lifecycles and
schema export from the built provider. Terraform versions other than 1.17.0 have
not been validated. The manifest declares protocol 6.

Never replace a released artifact; make a new version. Keep signing keys,
Registry publication and live tenant validation separate from secret-free
provider checks. SBOM/attestation policy is a release-packaging decision and is
not claimed here as a universal Terraform Registry requirement.
