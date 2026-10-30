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

Required evidence:

- The distinct removal method/path and exact payload, successful response, and
  already-absent behavior for classic Blueprints and Assignment Maps.
- A supported read endpoint exposing each Library Item's assignment-node
  placements, with pagination, identity and propagation semantics.
- If no node-level read exists, the supported import/drift contract for a
  declarative assignment integration.

The provider does not guess a removal route, treat assignment as a toggle, or
claim node-specific refresh/import verification from Blueprint-wide membership.

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

## Detached installer artifacts

Custom App and IPA upload/validation workflows are documented, but detached
artifact deletion/abort/garbage-collection is not. Clarify upload lifetime,
orphan enumeration/deletion and processing-readiness for Custom Apps. Library
Item deletion is implemented separately and is not described as artifact cleanup.

## Verification requirement

Clarified documentation permits implementation; it does not prove acceptance.
Each supported operation still needs immutable provider/caller revisions and
actual isolated-tenant results. Passing unit, protocol or mocked CLI tests is
not live CRUD evidence. Device enrollment and one-shot operational commands are
distinct from ownership of configuration settings.
