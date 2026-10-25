<!-- SPDX-License-Identifier: MPL-2.0 -->

# Contributing

Keep changes limited to the documented Iru API. Include a regression for behavior
changes and explain API evidence, state impact, and test results in the PR.
Use synthetic fixture values; never record real tokens, tenant payloads, device
identifiers, or unredacted error bodies. Preserve upstream notices for any reused
source and identify its license before adding it.

## Manual checks

Use verified local Go and Terraform executables; the scripts do not install them
or change user/system PATH. From the repository root:

```powershell
./scripts/verify.ps1 -GoExe <path-to-go.exe> -TerraformExe <path-to-terraform.exe>
```

The command checks formatting, module integrity, a CGO-disabled trimmed build,
unit/Framework protocol tests, loopback-only CLI tests, and example formatting.
Vet is a separate required gate. On the current host, application control blocks
the official vet executable. Use `-SkipVet -VetSkipReason "Application control
blocks vet"` only to run independent checks; skipped vet is not a passing check.
Do not disable security policy to complete it. Without `-TerraformExe`, the CLI
test is skipped and must be reported as not run.

Generate documentation with the pinned HashiCorp tool after exporting the
provider schema, as described by `scripts/generate-docs.ps1`. Commit the two
generated reference pages together with their templates/examples. Re-generating
must produce no differences before a change is approved. Do not edit generated
reference output directly. Internal research and validation working notes do not
belong in public provider documentation.

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
The candidate cross-platform matrix is Linux/macOS/Windows on amd64/arm64; only
Windows amd64 has been exercised locally, so the other targets are not yet
claimed as supported. The manifest declares Framework protocol 6.

Signing keys, Registry access, workflows, release publication and real tenant
access require separate authorization. Never replace a released artifact; make
a new version. SBOM/attestation policy can be decided for release packaging and
is not claimed here as a universal Terraform Registry requirement.
