<!-- SPDX-License-Identifier: MPL-2.0 -->

# Contracts needed for complete configuration migration

These questions identify missing or contradictory public API evidence. They are
not claims that no additional API exists. Sources were checked on 2026-10-29:
[rendered reference](https://api-docs.iru.com/) and its
[published Postman collection](https://api-docs.iru.com/api/collections/15284493/TzCTZkBe?segregateAuth=true&versionTag=latest).
No tenant data or credentials are needed to clarify these contracts.

## Library Item removal and node-level assignment state

The collection represents both assignment and removal with the same POST path
and the same Library Item/node input fields. The rendered reference does too.
The raw URL, parsed URL path, and saved original-request examples all agree on
that conflicting path:

| Request | Collection request ID | Method/path |
| --- | --- | --- |
| Assign Library Item | `683ac930-5754-4203-b066-59cdb8ced781` | `POST /api/v1/blueprints/:blueprint_id/assign-library-item` |
| Remove Library Item | `0d968198-fd6c-44ef-a3e0-a75e0a0adf71` | `POST /api/v1/blueprints/:blueprint_id/assign-library-item` |

The membership GET request (`34af40a5-7273-425c-9ea5-2c8a5dbbe29f`)
returns Library Item ID/name entries, without node IDs. It can verify Blueprint
membership but cannot prove a particular Assignment Map node placement.

First-party permission UI inspection on 2026-10-31 verifies the distinct removal
route as `POST /api/v1/blueprints/{blueprint_id}/remove-library-item`. The documented
body supplies `library_item_id` and, for map nodes, `assignment_node_id`; examples
return UUID arrays. The classic-root resource omits node placement, verifies the
Blueprint type and complete list before/after writes, and avoids a POST when the
desired membership already holds. It does not infer membership from a success
array, guess DELETE, or treat collection404 as an absent pair. Live convergence,
processing/propagation and error semantics remain unverified for this new resource.

Still required for Assignment Maps: a supported node-aware read/import/drift/remove
contract, including multiple placements of one item, pagination, identity and
propagation. A flattened list cannot prove which node owns an assignment. All map
placement is therefore refused by this resource, not falsely claimed tested.

## Built-in Library Items, parameters and map/routing definitions

The published collection has CRUD for Custom Scripts, Custom Profiles, Custom
Apps and In-House Apps. Its Blueprint writes list container metadata/enrollment
fields and cloning inputs; its routing writes expose enrollment settings.

Complete UI-to-code migration still needs supported schemas and endpoints for
other built-in Library Item types, classic parameters, Assignment Map nodes and
conditions, and routing rule definitions. Required details include create/read/
update/delete, versioning, assignment references and import/drift semantics.
Read-only fields in a response do not establish a writable contract.

## ADE-device clearing and override observability

The ADE-device update description explicitly documents Blueprint/routing
switching and null `override_dep_profile_id` for relinking. It does not give
equivalent null-clearing rules for ADE user/asset assignments. Enrolled-device
null-clearing is documented separately and is not assumed to apply to ADE.

The ADE detail example also omits the override property while newer list/update
examples show it. Needed evidence: nullable user/asset setter behavior, and
whether omission of the override property means unsupported, unassigned or
temporarily unavailable. Until then, override management requires an explicit
reported field, and undocumented ADE null-clearing is refused.

Live read-only validation also found an ADE detail response whose `user_id` is
a positive JSON integer, while official read/write examples use directory UUIDs.
The ADE data source preserves that legacy identity as a decimal string without
mapping it to a directory UUID. Settings management still requires the documented
UUID contract and refuses this legacy representation. Numeric user writes,
cross-identity mapping and ADE assignment changes have not been validated.

ADE integrations separately document Blueprint Routing assignment and now have
that input/state support. The collection's saved responses and multipart bodies
remain fixed-Blueprint examples. The provider accepts explicit documented
routing response fields and legacy nested fixed-Blueprint metadata, and encodes
multipart null selection as an empty field plus an explicit routing flag. Real
create/renew/import acceptance of those representations still needs verification.

## Tag collection failure versus object absence

Tags have no documented detail GET. Only a completed, valid, bounded listing
that omits the captured UUID proves the Tag absent. A collection or later-page
HTTP404 is a failed read and must retain Terraform state, just like authorization
failure or malformed success data. This distinction is verified with local
Terraform fault injection; no live endpoint failure was induced.

## Detached installer artifacts

Custom App and IPA upload/validation workflows are documented, but detached
artifact deletion/abort/garbage-collection is not. Clarify upload lifetime,
orphan enumeration/deletion and processing-readiness for Custom Apps. Library
Item deletion is implemented separately and is not described as artifact cleanup.

The documented HTTP 503 can mean a Custom App upload is still processing. The
provider previously attempted its first Library Item write immediately after S3
accepted a source upload. Iru's first-party irupkg workflow instead waits five
seconds at that boundary. The provider now includes that cancellation-aware
initial grace, tested locally; it is not a readiness guarantee or a live-proven
resolution. Two inactive test creates returned HTTP 503; subsequent complete
inventory/state reconciliation found no Library Item from either attempt.

The checked public Custom App API lists no upload-readiness GET. The IPA status
route is a different contract. A 503 alone does not establish that a write was
side-effect-free, and free-form response details were deliberately not retained
from those live failures. Do not describe them as a confirmed service outage or
automatically replay a write. A verified operation-specific processing response
and duplicate-prevention contract would be needed for a bounded write retry.
References: [API response codes](https://api-docs.iru.com/) and
[first-party irupkg workflow](https://github.com/kandji-inc/irupkg/blob/3fe1ec778c9e9fe7e7f7308dc8785c90e431df68/src/irupkg/helpers/utils.py).

## Verification requirement

Clarified documentation permits implementation; it does not prove acceptance.
Each supported operation still needs immutable provider/caller revisions and
actual isolated-tenant results. Passing unit, protocol or mocked CLI tests is
not live CRUD evidence. Device enrollment and one-shot operational commands are
distinct from ownership of configuration settings.


## Custom Script remediation removal

Live field testing on an inactive, unassigned disposable Script accepted nonempty
remediation but rejected a subsequent empty `remediation_script` PATCH with
HTTP 400. A verified alternative removal contract is unavailable. The vendor
SDK filters null values to omission; its mocked repository tests assume empty
clearing, which does not override the live rejection. Do not guess null, an
undocumented endpoint, or replace/delete an existing Library Item to simulate a
field clear. Planning and updates currently refuse a known nonempty-to-empty
transition before transport; retain existing remediation until a contract is
verified. Creation without remediation and unchanged-empty metadata updates
remain supported.
