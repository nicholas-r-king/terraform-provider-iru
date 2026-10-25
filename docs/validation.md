<!-- SPDX-License-Identifier: MPL-2.0 -->

# Milestone validation status

Validated on Windows on 2026-10-25, branch `feat/provider-foundation`.

## Result

The production executable builds. All 48 tests/subtests pass with no test skips
or failures, including a real Terraform CLI lifecycle against a loopback-only
mock. Vet remains blocked by Windows application control, so the ordinary
`go test ./...` pipeline is not reported as passing. No Iru tenant was contacted.

| Check | Final status |
| --- | --- |
| Official Go/Terraform archive SHA256 verification | Passed |
| `go mod tidy` | Passed; go.mod and go.sum recorded |
| `go mod verify` | Passed; all modules verified |
| `gofmt` and final `gofmt -l` check | Passed |
| `go build -o <workspace artifact> .` | Passed |
| `go test -vet=off -count=1 ./...` with local Terraform enabled | Passed; 48 tests/subtests, zero failed/skipped tests |
| Terraform `validate` and `providers schema -json` on production executable | Passed; token/script attributes sensitive |
| Terraform `fmt -check -recursive examples` | Passed |
| `git diff --cached --check` | Passed |
| Vet / default `go test ./...` | Blocked before test execution by OS application control |
| Live acceptance tests / real tenant apply | Not run; out of authorized scope |

The root Go package has no tests; Go reports that package as skipped. No actual
test or subtest was skipped in the enabled final run.

## Verified local tools

Downloaded and extracted only inside the task workspace after user approval.
No installer ran, no persistent Go settings were written, and neither user nor
system PATH changed. Go caches and module downloads also remain in the workspace.

| Tool | Official source | Published/verified archive SHA256 |
| --- | --- | --- |
| Go 1.27.3, windows/amd64 | `https://go.dev/dl/go1.27.3.windows-amd64.zip` | `f26081f438319757f8a1b46d7d0b2c5ec7583d4038e485e0aa725cfde7885fff` |
| Terraform 1.17.0, windows/amd64 | `https://releases.hashicorp.com/terraform/1.17.0/terraform_1.17.0_windows_amd64.zip` | `2948dd7f77dec0f562ec29000103e0441d915be5e5f1a02c1ce23681951cfb19` |

Go's checksum came from `https://go.dev/dl/?mode=json`. Terraform's checksum came
from its official `terraform_1.17.0_SHA256SUMS` file. Both archive hashes matched
before extraction or execution. Vendor license files remain with the extracted
tools. The source module targets Go 1.25.0 and pins Framework v1.19.0 / Plugin Go
v0.31.0; Go 1.27.3 was the actual validation compiler.

Relative to the checkout, tools are in `../.tools/`, the production build is
`../.tools/artifacts/terraform-provider-iru.exe`, and support evidence is in
`../.cache/go-test-license.jsonl` and `../.cache/production-cli-check/schema.json`.
Tool archives, caches, binaries, temporary mock state, and debug logs are not
staged as source deliverables.

## What the tests establish

- Config/environment precedence, invalid and unknown configuration, schema
  sensitivity, and request origin validation.
- Exact documented HTTP methods/paths/JSON, explicit false and empty values,
  execution enums, and Self Service dependencies.
- Bearer headers; no token or response-body content in diagnostics/client
  formatting; no synthetic token in the CLI debug log.
- Redirect refusal, finite timeouts, bounded GET retries/Retry-After handling,
  cancellation, no write replay, invalid IDs, malformed/oversized responses,
  and response-ID checks.
- UUID import/read reconstruction, stable repeated refresh, no-op framework
  plans, 404 removal, preservation of prior state on other errors, and retention
  of a known ID from a malformed create response.
- Actual Terraform CLI validation/schema export, uppercase UUID import,
  no-op plans, planned changes, synthetic PATCH/POST/DELETE, and removal of state
  after the mock returns 404. Invalid frequency fails CLI validation.
- Review regressions reject empty-fragment API URLs, preserve equivalent UUID
  spelling while retaining real category drift, and expose static-default
  changes after a nondefault import. Explicit matching import configuration
  remains no-op, and import/planning perform no mutations.

The CLI test copies the Go test binary as a temporary provider. Only that test
binary recognizes helper environment variables. Its transport refuses anything
except the fixed synthetic hostname/token and maps requests to an ephemeral
`127.0.0.1` mock. The normal executable has no configuration or environment
option to replace the transport. No system TLS trust, firewall, or security
policy was changed. The production executable was separately validated and its
schema inspected with Terraform.

## Failures encountered and disposition

- The initial default Go test run, and its approved execution outside the
  sandbox, were both blocked while starting the official
  `../.tools/go/pkg/tool/windows_amd64/vet.exe` with: "An Application Control
  policy has blocked this file." That action is **blocked**, not pending or
  uncertain. No attempt was made to disable/unblock policy, rename the tool,
  rebuild it, or claim vet success. Tests then ran independently with `-vet=off`.
- The first CLI harness attempt failed because the sandbox denied Terraform's
  loopback RPC connection to its provider process. The supported approved
  execution outside the sandbox resolved it; final CLI checks passed.
- Client formatting was hardened so both pointer and value formatting redact
  credentials. Tests cover both. No documented API-field mismatch or compiler
  failure remained in the final run.
- A fresh independent review confirmed two P2 defects (empty-fragment URL path
  construction and category UUID case consistency); both were reproduced and
  fixed. A P3 documentation clarification explains imported static-default
  changes. See [review.md](review.md). The 48-test run and build were repeated
  after fixes; vet was not retried or bypassed during this review.
- The approved MPL-2.0 license and SPDX source notices were added without Go
  behavior changes. The build, 48-test suite, and local CLI/example checks were
  repeated successfully; vet remained blocked and was not retried.

## Repository and publication status

The implementation was developed from an empty repository using public API and
framework references. It is proposed on `feat/provider-foundation` in draft PR
\#1; the main branch retains only a minimal baseline and its license. No provider
implementation has been merged, and no release or Registry publication exists.

The owner selected MPL-2.0 for the original source. A read-only audit of both
published branches, their two original commits, the 18-file milestone, and PR
metadata found no credentials, actual tenant/customer data, personal filesystem
paths, or confidential source. Literal tokens, UUIDs, and tenant-like hostnames
in tests are synthetic. Nonsecret local machine/setup references were removed
from current documentation; earlier commits retain those nonsecret references.
No sensitive history requiring a rewrite was identified.

## Remaining requirements

1. An administrator-approved resolution of the OS block is needed to complete
   vet. Do not weaken security policy just to run this milestone.
2. Review source and documented legacy-response limits before use. Before any
   binary release, package dependency license/copyright/NOTICE material and MPL
   source-availability information as described in THIRD_PARTY_NOTICES.md.
3. A later explicitly authorized nonproduction tenant check is still needed for
   actual API permissions, normalization, Self Service transitions, remediation
   clearing, propagation, and tenant-level import/no-op behavior. Passing mocks
   do not establish production readiness.
