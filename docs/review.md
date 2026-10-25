<!-- SPDX-License-Identifier: MPL-2.0 -->

# Independent milestone review

Completed on 2026-10-25 by a fresh, read-only reviewer in this environment.
The reviewer inspected the current source, tests, and recorded official API
contract. The implementation owner added fixes and regression tests. No live
Iru request, credential lookup, publication, or vet execution was part of review.

## Findings and disposition

| Severity | Finding | Resolution |
| --- | --- | --- |
| P2 | A URL ending in an empty fragment delimiter passed validation. Because the original string was concatenated with the API path, that path became a fragment and HTTP targeted `/` instead. The host allowlist still prevented off-origin forwarding. | Fixed: reject every fragment delimiter and construct requests from a canonical validated scheme/host origin. Regression tests first reproduced the rejection failure, then passed. |
| P2 | When configuration used an uppercase Self Service category UUID and Iru returned the same UUID in lowercase, Create/Update replaced configured spelling in state. Terraform could report an inconsistent apply result, and refresh could introduce case-only differences. | Fixed: preserve plan/prior spelling only when the UUIDs compare equal ignoring case. Tests cover Create, Read, Update, and a genuinely different category that must still appear as drift. The regression reproduced the original failure before the fix. |
| P3 | Import guidance understated static defaults. Omitting nondefault imported attributes can plan deactivation, remediation clearing, frequency/restart changes, or hiding from Self Service. | Clarified README: import makes no writes, but accepting a later default-changing plan does. A nondefault import regression verifies that explicit matching configuration is no-op, omission exposes default changes in the plan, and neither import nor planning mutates the mock. Static default semantics are unchanged. |

The reviewer independently confirmed the two code fixes and found no additional
high-confidence transport, retry, secret-handling, or lifecycle defect. The
implementation owner completed the documentation correction and additional
nondefault-import regression after that review.

## Reviewed controls and remaining limits

- Strict tenant-origin allowlist and refused redirects keep bearer credentials
  on the configured official API origin. Errors withhold raw HTTP/transport
  content, and client pointer/value formatting is redacted.
- Script/token attributes are sensitive; script content still resides in
  Terraform state. No HTTP body or authentication logger is implemented.
- Mutations have one application-level attempt; GET retries are bounded. If a
  creation reaches Iru but returns no usable ID, manual inspection/import remains
  necessary before retrying. No documented idempotency key was invented.
- Null/unknown configuration dependencies are deferred during validation and
  resolved values are checked before mutation. Read reconstructs managed fields;
  404 removes state, while other read errors preserve it.
- No Blueprint/Assignment Map/device endpoint is implemented. Static defaults
  remain visible plan intent; adopting existing settings requires matching HCL.
- Mocked results do not establish actual API permissions, normalization,
  propagation, Self Service transitions, or remediation clearing. Ambiguous
  legacy `no_enforcement` responses remain explicitly unsupported.

## Verification

See [validation.md](validation.md) for final compiler/CLI versions, verified
archive checksums, command results, and the OS application-control blocker.
Review checks use `go test -vet=off`; vet remains blocked and was not retried or
bypassed during review. No system security policy changed.
