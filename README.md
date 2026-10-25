# Terraform provider for Iru — development milestone

Local, unpublished provider foundation using the HashiCorp Terraform Plugin
Framework v1.19.0. The first resource is `iru_custom_script`.

This is a review milestone, not production-ready software. It has not contacted
an Iru tenant. The build and synthetic tests pass; Windows application control
blocks vet. See [validation status](docs/validation.md) before running it.

## Scope

- Documented Custom Scripts POST, GET by UUID, PATCH, and DELETE operations.
- Import by Library Item UUID, complete refresh of managed attributes, and 404
  refresh removal. A 404 during delete is also treated as already absent.
- Audit/remediation content, execution frequency, active/restart flags, and
  documented Self Service settings. No assignments or device operations.
- Config validation, bounded read retries, finite request timeouts, refused
  redirects, and errors that withhold authentication and script content.
- Synthetic transport/framework unit tests and an opt-in Terraform CLI test
  against a loopback mock. No live acceptance test is included.

## Authentication

Set `IRU_API_URL` to your HTTPS tenant API origin and load `IRU_API_TOKEN` through
your existing secure secret workflow. Do not put a token into committed HCL,
shell history, a `.tfvars` file, an issue, or a chat. This provider does not create
tokens or change permissions. The token must already permit the relevant Custom
Scripts operations.

Supported origins are `https://<tenant>.api.iru.com`, `.api.eu.iru.com`,
`.api.kandji.io`, and `.api.eu.kandji.io`. Use the actual API origin supplied by
Iru; paths, alternate hosts, ports, query strings, credentials, and fragments are
rejected. Explicit `api_url`/`api_token` arguments override environment values;
an explicitly empty value is an error.

The token argument is sensitive. Script attributes are also sensitive to reduce
casual display, but **script contents are still stored in Terraform state**.
Keep credentials out of scripts and protect state through your normal workflow.
The client emits no HTTP request/response logs, including under `TF_LOG`.
Terraform's own diagnostic/debug output should still be treated as sensitive.

## Development

Requires Go 1.25 or later. A current stable Go toolchain is preferred. The pinned
framework requires Go 1.25. Terraform CLI is needed for subsequent CLI-level
validation, but the unit tests use the framework protocol directly and need no
Terraform installation.

```powershell
go mod tidy
go fmt ./...
go test ./...
go vet ./...
go build -o terraform-provider-iru.exe .
```

To include the synthetic CLI lifecycle test, set `IRU_TEST_TERRAFORM` to a
verified local Terraform executable before running the tests. It uses a test-only
provider binary, synthetic credentials, a temporary state directory, and an IPv4
loopback mock. The production binary has no mock flags or environment bypass.
The test needs local provider RPC sockets; a restricted sandbox may deny them.
Current NK-Main results used `go test -vet=off` because application control blocks
the official Go vet executable. This does not count as a passing vet check.

For a local CLI evaluation after these checks pass, configure a Terraform CLI
`dev_overrides` entry for `registry.terraform.io/nicholas-r-king/iru` pointing at
the directory containing the built provider. The address is a development
identity; no registry publication or release exists. Do not expect `terraform
init` to download this unpublished provider.

The [example](examples/custom_script/main.tf) contains only a synthetic inactive
script. Terraform apply/destroy ran only against the synthetic loopback fixture.
No real-tenant apply has run or been authorized in this milestone.

## Import and state

```hcl
import {
  to = iru_custom_script.example
  id = "11111111-2222-4333-8444-555555555555" # synthetic; replace before use
}
```

Import only records the UUID. Refresh then reads and populates all managed
fields. Write explicit matching values for imported nondefault settings before
accepting a plan. Omitting optional attributes applies this resource's static
defaults: it can plan to deactivate an item, clear remediation content, change
frequency to `once`, disable restarts, or hide it from Self Service. Import itself
does not write to Iru; accepting a later plan with those changes will. A no-op
import requires configuration that matches the remote settings.

`no_enforcement` requires Self Service and a valid existing category UUID.
Category and recommendation settings are managed only while Self Service is on.
When switching it off, omit the category and set recommendation false. Hidden
category metadata is omitted from writes and normalized out of state; it is not
claimed to be erased remotely. Legacy GET responses with `no_enforcement` but
without Self Service metadata fail with a clear diagnostic because their state
cannot be reconstructed safely.

Scripts and names are preserved verbatim. The provider does not normalize
whitespace or line endings. UUID imports are canonicalized to lowercase. Script
content must be nonempty; `remediation_script = ""` sends an empty remediation
string. Synthetic tests cover this mapping; tenant behavior still needs a later
explicitly authorized nonproduction verification.

Equivalent category UUID case returned by Iru preserves the configured/prior
spelling in state. A genuinely different category UUID is still recorded as drift.

Only GET retries 429/502/503/504, at most three attempts. Retry-After is honored
within a five-second wait limit; longer or invalid values return an error. Writes
are never replayed automatically. If creation times out or returns an invalid
response, it may have succeeded: inspect the tenant and import the existing item
before retrying. A valid ID from a malformed create response is retained in state
when possible.

## Sources and licensing

[API contract and provenance](docs/api-contract.md) identifies the official API
collection, vendor CLI reference, and community options reviewed. Implementation
and test fixtures were written for this repository; no vendor/community source
was copied or vendored. No project license has been selected. Dependency licenses
apply to those dependencies and must be reviewed before distribution.
