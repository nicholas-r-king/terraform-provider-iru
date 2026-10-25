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
On the current Windows validation host, application control blocks vet. Only on
such a restricted host, use `-skip-vet-reason "Application control blocks vet"`
to run independent checks; skipped vet is not a pass. Do not disable security
policy. Vet remains required on an unrestricted Linux/macOS validation host.

Generate or check the two reference pages with the pinned official HashiCorp
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
  Pagination is needed only if a documented list endpoint is introduced.

## CI and release gates

No CI/release workflow is enabled by this milestone. A later approved CI setup
should run format/build/vet/lint, unit and mocked Terraform acceptance tests on
declared supported versions/platforms, plus a generated-doc clean-diff check.
Pin Actions to immutable SHAs, use minimum read permissions for validation, keep
fork PR jobs free of secrets, and separate any authorized live tests/releases.

Before a Registry release: choose a SemVer tag and changelog, verify the declared
Go/Terraform minimums, build and smoke-test each supported OS/architecture,
package applicable license/copyright/NOTICE material and MPL source locations,
and create correctly named ZIPs, protocol manifest, SHA256 sums and checksum
signature under the current [publishing requirements](https://developer.hashicorp.com/terraform/registry/providers/publishing).
Linux/macOS amd64 and arm64 are the primary target matrix; Windows is optional.
The provider, contributor tool and mock CLI test binary cross-compile for all four
Linux/macOS targets with Go 1.27.3 and CGO disabled. Only Windows amd64 has been
runtime-tested so far; cross-compilation does not establish runtime compatibility.
Before claiming Linux/macOS support, run
`go run ./tools verify -check-docs` natively on those targets, including the
loopback mock lifecycle and a built-provider install/schema smoke test. Then test
the declared minimum Go/Terraform versions. The manifest declares protocol 6.

Signing keys, Registry access, workflows, release publication and real tenant
access require separate authorization. Never replace a released artifact; make
a new version. SBOM/attestation policy can be decided for release packaging and
is not claimed here as a universal Terraform Registry requirement.
