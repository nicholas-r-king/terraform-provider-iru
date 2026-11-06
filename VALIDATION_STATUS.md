<!-- SPDX-License-Identifier: MPL-2.0 -->

# Validation status

Updated 2026-11-06. This branch has extensive local automated coverage and selected
live evidence from earlier development builds. It has not completed full live
acceptance.

## Automated checks

- Full contributor checks passed on macOS arm64 with Go 1.27.3 and Terraform
  1.11.0/1.17.0: unit/protocol/real CLI mock tests, vet, dependency integrity,
  formatting and deterministic docs.
- The final installer-validation follow-up passed 147 selected test/subtest events
  on each of Terraform 1.11.0, 1.11.1 and 1.17.0. The prior audit passed all 73
  native Python/mock/CLI runner tests and client race checks.
- Darwin/Linux amd64/arm64 builds passed; repeated native builds matched.
  Cross-compilation is not execution on the other platforms.
- Tests cover unknown/null validation, imports, no-op/drift, bounded pagination,
  response validation, redaction and state retention after failed writes.
  Ambiguous writes are not automatically replayed. A missing response ID may
  leave a committed remote object outside state and requires reconciliation.

Remote checks for a particular commit must be inspected separately; skipped CI
is not a passing check.

## Selected live evidence and remaining tests

| Family | Selected evidence from earlier builds | Still unverified |
| --- | --- | --- |
| Script | Inactive lifecycle, import/no-op, drift/repair/delete; selected frequency/content transitions | Self Service, execution/device effects, and a supported remediation-clear operation. |
| Profile | Inactive lifecycle, import, selected XML/platform transitions and cleanup | Rich/signed/binary payloads and actual enforcement. |
| Custom App | Inactive, unassigned package replacement and ZIP lifecycle, import/no-op/drift/repair/delete; independent cleanup | DMG, remaining fields/type transitions, Self Service, upload readiness, installation and device effects. Uploaded files may remain. |
| In-house App | Automated coverage only | Signed-IPA live lookup/upload/lifecycle. |
| Tag / Note | Selected CRUD, import/drift/repair and deletion; Note tests used one designated device | Remaining normalization, replacement and fleet/device effects. |
| Blueprint | One empty Map container: create/metadata/import/drift/repair; retained with enrollment off and no assignments | Cloning, activation/code transitions and graph/placement. Remote DELETE is blocked. |
| ADE integration | Configuration lookup and token-free no-change import planning | Creation, renewal, persisted adoption and assignment using an isolated fixture. Remote DELETE is blocked. |
| Enrolled device settings | Designated device asset-tag change and restoration | Blueprint/user/tag transitions and broader device effects. Destroy only releases management. |
| ADE device settings | Metadata and a GET-only empty-tags observation | Managed field transitions, including tag writes. |
| Routing / classic assignment | Local mocks; Routing tenant returned 404 | Configured Routing lifecycle. Current Map assignment remains unsupported. |
| Data sources | Selected Blueprint/library/inventory/device/user/bootstrap and Script/Profile/App/ADE configuration reads | New status snapshots, In-house configuration, configured Routing and remaining response variants. New fields do not inherit older live evidence. |

An earlier App repair failure remains unexplained despite later passing runs and
verified cleanup. A separate old Script import-state lock also remains unresolved;
neither is erased by a newer mock pass. Detailed immutable-build evidence remains
in the private development history.

## Use and release limits

Review imported defaults before applying. Protect retained Blueprint/ADE objects
with `prevent_destroy`: provider DELETE refusal does not stop every tainted
create-before-destroy path. Existing saved plans from older providers are not
certified. Terraform 1.11.0 has an upstream ephemeral+sensitive serialization bug;
use 1.11.1 or later for that input combination.

The remaining unsupported contracts are listed in [API coverage](API_COVERAGE.md).
