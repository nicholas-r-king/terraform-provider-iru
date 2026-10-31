<!-- SPDX-License-Identifier: MPL-2.0 -->

# API coverage and migration requirements

Coverage is based on the [official Iru Endpoint Management API collection](https://api-docs.iru.com/api/collections/15284493/TzCTZkBe?segregateAuth=true&versionTag=latest),
rechecked 2026-10-31. An API endpoint existing does not establish that it works with
a particular tenant or token. Synthetic tests and live acceptance are separate.
Unresolved public contracts and the evidence needed to implement them are listed
in [API contract gaps](API_CONTRACT_GAPS.md). The complete current argument/data-source inventory and evidence limits are in [validation status](VALIDATION_STATUS.md).

| Object | Documented contract | Provider support |
| --- | --- | --- |
| Custom Script | CRUD | Baseline/import/drift/delete and selected field variants passed live; clearing existing remediation is blocked after HTTP 400 rejection |
| Custom Profile | CRUD | Baseline/import/drift/delete and inactive content/platform variants passed live; activation and real payload enforcement remain unverified |
| Custom App | CRUD, upload | Candidate221ac4a inactive/unassigned PKG create/import/drift/repair/delete and independent cleanup passed; selected script/enforcement/restart variants passed in an earlier run whose later repair failure remains unresolved. Existing-key/replacement/Self Service/installation coverage remains incomplete |
| Blueprint | CRUD, clone | Metadata/enrollment resource, UUID lookup; nodes/parameters excluded |
| Blueprint template catalog | Read only; grouped positive integer IDs | Bounded catalog lookup projects IDs/names/categories for source_type=template cloning; parameters are not made writable |
| Tag | CRUD; read via paginated list | Resource; import, drift and disappearance handling |
| In-House App | CRUD, upload and upload status | Existing key or local streaming upload with bounded IPA validation; live lifecycle unverified |
| ADE integration | CRUD, token renewal, Blueprint/routing assignment | Resource with write-only token input and local renewal version; documented routing/Blueprint choices supported; multipart/response routing details remain live-unverified |
| ADE public key | Read-only ABM bootstrap PEM | Public-only certificate/key lookup with exact-response SHA256; no private material or trust-chain assertion |
| Device Note | CRUD | Composite UUID import; pilot-only baseline/Unicode/drift/repair/delete passed; other normalization/replacement variants remain unverified |
| Library Item placement | Assign/remove POST; complete aggregate membership list | Classic-root pair resource with composite import, read-before-write idempotence, verified add/remove convergence and local wire/Terraform tests. No live assignment acceptance yet. All Assignment Maps refused until node placement is observable |
| Blueprint routing | Read and update enrollment settings | Settings resource and sensitive lookup; matching adoption/import is read-only, destroy relinquishes management; live validation pending. Routing rule editing is not documented |
| Device/ADE device metadata | Read and PATCH selected fields | Settings resources and exact-UUID lookups. Unconfigured fields stay unmanaged; destroy releases management without deleting devices. The ADE lookup preserves reported legacy numeric user IDs as decimal strings, without treating them as directory UUIDs or enabling numeric writes. ADE override management requires getter support; undocumented ADE null-clearing remains a contract gap. Live field acceptance incomplete |
| Self Service category | List only | ID/name lookup through configuration inventory; no documented create/update/delete |
| Directory user | Read/delete only | Exact-UUID identity/status lookup. Creation/update belongs to the directory integration; no destructive user lifecycle resource is invented |
| Device inventory, activity, status, licensing, audit, vulnerability data | Read-oriented API | Reporting scope; does not provide configuration CRUD |
| Device actions | Operational commands, including some persistent-setting setters | Not currently modeled. Naming/preferences need verified async/read/convergence semantics before a declarative resource; enrollment/deletion/erase are not CRUD equivalents |
| Other built-in Library Item types, Assignment Map conditions and parameters | No verified complete public CRUD contract in this collection | Still required for full configuration coverage; contract discovery and implementation remain open, with no invented endpoints |

## Completion criteria

Each manageable object needs create, refresh, update, import, a no-change plan
after apply/import, drift repair, delete, and disappearance handling. Its tests
must cover real request encoding, explicit false/clear values, failures that
retain state, partial creation identity recovery, and writes that are not replayed.
Live evidence must identify the immutable provider and consumer revisions and
each completed lifecycle phase; a mock pass is not a live pass.

Migration should begin with read-only inventory and configuration generation,
followed by import into protected state and inspection of the resulting plan.
Import itself must not activate, reassign, recreate, or delete an existing item.
Static defaults can plan changes to imported settings: configure all existing
values that must be retained before applying. Use `prevent_destroy` for production
objects when appropriate. Application installers and XML/script content can
remain in state even when their schema fields are marked sensitive.
