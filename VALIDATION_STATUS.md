<!-- SPDX-License-Identifier: MPL-2.0 -->

# Validation coverage: draft, not production acceptance

Updated 2026-10-31. Runtime evidence is tied to candidate `221ac4a` or explicitly older binaries; a later documentation/test commit does not reattribute that evidence. Published preview `0.1.0-alpha.1` predates the draft safety/API fixes. Full configuration coverage and production adoption remain open.

This audit was reconciled against an actual Terraform schema export of candidate-equivalent source: **12 resources / 73 configurable resource arguments; 9 data sources / 7 selector arguments; 2 provider settings**. Required and optional arguments are counted, including optional-computed and write-only inputs. Computed-only output fields are not configuration primitives. Every configurable name appears below exactly once. Counts establish inventory, not passing coverage.

“Selected live” means the stated values/phases only. “Baseline only” never counts a false/null/single-value observation as a transition test. Local tests use synthetic APIs; they do not establish tenant compatibility.

## Resource fields and lifecycle limits

### `iru_ade_device_settings`

[Implementation](internal/provider/device_settings_resource.go) · [Representative local CLI/unit tests](internal/provider/device_settings_cli_test.go).

| Arguments | Evidence | Remaining limit |
| --- | --- | --- |
| `device_id`, `blueprint_id`, `user_id`, `asset_tag`, `use_blueprint_routing`, `override_dep_profile_id` | Local only; fixture/contract blocked | Metadata data source passed; managed settings did not. Need test-owned ADE record, approved assignment targets, observable override and update scope. Legacy numeric user read does not authorize numeric writes; undocumented user/asset clearing is refused. |

Computed-only outputs: `id`.

### `iru_ade_integration`

[Implementation](internal/provider/ade_integration_resource.go) · [Representative local CLI/unit tests](internal/provider/ade_routing_cli_test.go).

| Arguments | Evidence | Remaining limit |
| --- | --- | --- |
| `blueprint_id`, `use_blueprint_routing`, `email`, `phone`, `server_token`, `server_token_version` | Local only; fixture/security blocked | Need dedicated zero-device ABM server/token and exact create/update/renew scope. Existing tokens must not rotate. Write-only token/version is local bookkeeping, not remotely observable token drift. Delete/replacement fails closed. |

Computed-only outputs: `access_token_expiry`, `id`, `server_name`.

### `iru_blueprint`

[Implementation](internal/provider/blueprint_resource.go) · [Representative local CLI/unit tests](internal/provider/configuration_cli_test.go).

| Arguments | Evidence | Remaining limit |
| --- | --- | --- |
| `name`, `type`, `description`, `color`, `icon`, `enrollment_code`, `enrollment_active`, `source_type`, `source_id` | Local only; lifecycle blocked | Metadata/enrollment/create/clone/import paths tested synthetically. Need isolated empty classic/map/clone fixtures and create/update scopes. Type/provenance replacement and Delete fail closed. Map appearance omissions and published response-shape assumptions need live proof. Enrollment activation requires separate approval. |

Computed-only outputs: `id`.

### `iru_blueprint_library_item_assignment`

[Implementation](internal/provider/root_assignment_resource.go) · [Representative local CLI/unit tests](internal/provider/root_assignment_cli_test.go).

| Arguments | Evidence | Remaining limit |
| --- | --- | --- |
| `blueprint_id`, `library_item_id` | Local only; highest-value primitive gap | Classic-root create/adopt/import/no-op/case-only/change/drift/detach/failure retention tested locally. Need a dedicated empty classic Blueprint plus inactive owned items and assign/remove scopes. All maps, including simple-view maps, are refused until node placement can be read. |

Computed-only outputs: `id`.

### `iru_blueprint_routing`

[Implementation](internal/provider/blueprint_routing_resource.go) · [Representative local CLI/unit tests](internal/provider/blueprint_routing_cli_test.go).

| Arguments | Evidence | Remaining limit |
| --- | --- | --- |
| `enrollment_code`, `enrollment_active` | Local only; configured fixture blocked | Current live lookup returned404 while unconfigured. Need an authorized already-configured isolated target or explicit persistent-feature consequence approval. Destroy releases management; it does not remove routing or edit rules. |

Computed-only outputs: `id`.

### `iru_custom_app`

[Implementation](internal/provider/custom_app_resource.go) · [Representative local CLI/unit tests](internal/provider/installer_upload_cli_test.go).

| Arguments | Evidence | Remaining limit |
| --- | --- | --- |
| `name` | Selected live on 221ac4a | Focused source-file create/read/no-op/rename/import/drift/repair/no-op/delete/disappearance passed; independent inventory/state reconciliation verified cleanup. |
| `preinstall_script`, `postinstall_script`, `audit_script`, `install_enforcement`, `restart` | Selected live on 221ac4a | Eight inactive/unassigned set/clear, install_once/continuously_enforce and false/true variants passed in the earlier field run. That run failed later at repair planning; cleanup eventually verified. Other values and execution remain unverified. |
| `source_file`, `source_sha256`, `file_key` | Baseline only on 221ac4a | One reviewed README-only PKG hash uploaded and imported. Replacement bytes, same-bytes/different-path and configured existing-key lifecycle remain local only. |
| `install_type` | Baseline only | package only; zip/image fixtures and rules not live tested. |
| `active` | Baseline only | false only; no activation, device install or execution. |
| `show_in_self_service`, `self_service_recommended`, `self_service_category_id`, `unzip_location` | Local only | Publication/category variants and ZIP target path set/change/clear remain unverified. |

Computed-only outputs: `file_size`, `id`, `sha256`.

### `iru_custom_profile`

[Implementation](internal/provider/custom_profile_resource.go) · [Representative local CLI/unit tests](internal/provider/profile_normalization_cli_test.go).

| Arguments | Evidence | Remaining limit |
| --- | --- | --- |
| `name` | Selected live | Rename/import/drift/repair/delete passed on older candidate. |
| `profile` | Selected live | Empty PayloadContent and description set/clear passed; real nested/signed/large payload acceptance and enforcement remain unverified. |
| `runs_on_mac`, `runs_on_iphone`, `runs_on_ipad`, `runs_on_tv`, `runs_on_vision` | Selected live | Individual true/false transitions with at least one target retained passed on older candidate; omitted/default response variants and device effects remain unverified. |
| `active` | Baseline only | false only; no activated or delivered profile. |

Computed-only outputs: `id`, `mdm_identifier`.

### `iru_custom_script`

[Implementation](internal/provider/custom_script_resource.go) · [Representative local CLI/unit tests](internal/provider/custom_script_resource_test.go).

| Arguments | Evidence | Remaining limit |
| --- | --- | --- |
| `name`, `script` | Selected live | Rename/content/import/drift/repair/delete passed on older candidate; API-normalized outer whitespace required. Large/other normalization variants remain local. |
| `remediation_script` | Partial live; clear blocked | Nonempty set accepted; empty clear rejected HTTP400. No verified alternate removal contract; provider refuses it. |
| `execution_frequency` | Selected live | once/every_day/every_15_min passed; no_enforcement requires untested Self Service. |
| `restart` | Selected live | true/false passed on 433e62c. Remote item absent; stale import-state lock remains a separate cleanup obligation. |
| `active` | Baseline only | false observed; activation and actual execution not validated. |
| `show_in_self_service`, `self_service_recommended`, `self_service_category_id` | Local only | Publication, recommendation and category set/change/clear need reviewed categories and exposure scope. |

Computed-only outputs: `id`.

### `iru_device_note`

[Implementation](internal/provider/device_note_resource.go) · [Representative local CLI/unit tests](internal/provider/additional_objects_cli_test.go).

| Arguments | Evidence | Remaining limit |
| --- | --- | --- |
| `content` | Selected live | Pilot-only content/Unicode/import/drift/repair/delete passed; other HTML normalization, large/error variants remain local. |
| `device_id` | Baseline only | One exact pilot device; moving/replacing a Note across devices not live validated. |

Computed-only outputs: `id`.

### `iru_device_settings`

[Implementation](internal/provider/device_settings_resource.go) · [Representative local CLI/unit tests](internal/provider/device_settings_cli_test.go).

| Arguments | Evidence | Remaining limit |
| --- | --- | --- |
| `asset_tag` | Selected live | Pilot import/change/exact restore/release and retained settings verified; not complete settings acceptance. |
| `device_id` | Baseline only | Existing pilot adoption only; no device creation/deletion or selector replacement tested live. |
| `blueprint_id`, `user_id`, `tags` | Local only | Need exact safe targets and baseline-restoration approval. Tag values are names. Blueprint changes may affect policy; no fleet assignment tests. |

Computed-only outputs: `id`.

### `iru_in_house_app`

[Implementation](internal/provider/in_house_app_resource.go) · [Representative local CLI/unit tests](internal/provider/additional_objects_cli_test.go).

| Arguments | Evidence | Remaining limit |
| --- | --- | --- |
| `name`, `active`, `file_key`, `source_file`, `source_sha256`, `runs_on_iphone`, `runs_on_ipad`, `runs_on_tv` | Local only; fixture blocked | Mocked existing-key/upload/status/replacement/import/drift paths exist. No live IPA lifecycle. Need legitimate signed iOS/tvOS fixtures, update versions and exact deletion permission; empty tenant inventory is not acceptance. |

Computed-only outputs: `id`.

### `iru_tag`

[Implementation](internal/provider/tag_resource.go) · [Representative local CLI/unit tests](internal/provider/configuration_cli_test.go).

| Arguments | Evidence | Remaining limit |
| --- | --- | --- |
| `name` | Selected live | Metadata/import/drift/repair/delete/disappearance passed on older candidate. Duplicate/collision, large pagination and injected errors are local only. |

Computed-only outputs: `id`.

## Every data source and selector

| Data source | Configurable selectors | Live evidence | Remaining limit |
| --- | --- | --- | --- |
| `iru_ade_device` | `device_id` | Selected exact ADE record passed on 433e62c | Legacy numeric user ID preserved as metadata only; managed settings require documented UUID semantics. |
| `iru_ade_public_key` | None | Public PEM/digest plan passed on 221ac4a | No private key, token exchange, signing/trust or renewal acceptance. |
| `iru_blueprint` | `blueprint_id` | Selected metadata plan passed on 221ac4a | Type/name only; not parameters, map graph or managed Blueprint lifecycle. |
| `iru_blueprint_routing` | None | Live GET404; configured lookup unverified | Required configured fixture remains missing, not out of scope. |
| `iru_blueprint_templates` | None | Catalog plan passed on 221ac4a | Grouped template IDs/names; no template-clone acceptance. |
| `iru_device` | `device_id` | Pilot metadata plan passed on 221ac4a | One device; no assignment/command acceptance. |
| `iru_inventory` | `kind` | All eight kinds passed on 433e62c; seven required kinds repeated on 221ac4a | UUID/name projection, not full configuration export. In-house empty inventory is not an IPA lifecycle test. Large real pagination/fault induction remains untested. |
| `iru_library_item` | `blueprint_id`, `library_item_id` | Selected membership plan passed on 221ac4a | ID/name projection; not complete Script/Profile/App content or node placement. |
| `iru_user` | `user_id` | One selected directory UUID passed on 433e62c | Identity/status only; no directory mutation resource is invented. |

Provider settings: `api_url` and sensitive `api_token` have selected live authentication/origin evidence and local validation/default/environment tests. No credential rotation/grant is implied. Region/invalid-origin/error permutations remain synthetic. See [provider tests](internal/provider/provider_test.go).

## Failure, import and deletion semantics

- [Refresh fault tests](internal/provider/refresh_adversarial_cli_test.go) cover the original 11 resource kinds with401/403/malformed success and state retention; [assignment tests](internal/provider/root_assignment_cli_test.go) separately cover the twelfth kind, including collection failure and malformed-read retention. These are local, not induced tenant faults.
- Import/refresh/data-source reads must perform no API writes. Matching configuration is required: static resource defaults can plan changes after import; an omitted-default import is not automatically a no-op. Clone provenance and write-only token state cannot be reconstructed from a read.
- Blueprint and ADE integration deletion/replacement remain blocked, not passed CRUD. Device/ADE settings and Routing destroy relinquish management and retain remote settings. Assignment destroy detaches only the pair. Library Item deletion does not prove detached installer-file removal.
- Source uploads wait after S3 acceptance without automatically retrying a Library Item write. Selected live creates succeeded; this does not prove earlier503 causes or guaranteed processing readiness.
- One earlier candidate221 App field run failed at repair planning and left a Plan lock. Guarded cleanup later succeeded. A subsequent focused candidate221 lifecycle and independent reconciliation passed. The old cause remains unresolved; do not erase it or infer a fixed vendor/backend cause. The separate Script import-state lock remains unresolved.

## Missing base primitives: contract versus implementation

The [first-party collection](https://api-docs.iru.com/api/collections/15284493/TzCTZkBe?segregateAuth=true&versionTag=latest) was rechecked2026-10-31 (137 requests, published2026-05-15). This is evidence about that collection, not proof that no other supported API exists.

| Gap | Classification | Next evidence needed |
| --- | --- | --- |
| Assignment Map nodes/conditions and node memberships | Missing verified complete public contract; not implemented | Node-aware read/import, multiple placements, graph/versioning and exact add/remove convergence. Classic-root support does not cover simple-view maps. |
| Classic parameters and other built-in Library Item types | Missing verified complete public CRUD contract; not implemented | Per-type schemas/setters/deletion, identities and assignment references. Response fields do not establish writable fields. |
| Routing-rule definitions | Missing verified public CRUD contract; not implemented | Current resource covers enrollment settings only, not rule order/criteria/targets. |
| Self Service category lifecycle | Only list contract verified; no CRUD resource | Supported creation/update/deletion contract, references and retention behavior. Category lookup is already available. |
| Full Script/Profile/App/In-house/ADE configuration lookups or export | Read APIs/client models exist; provider data-source/export gap | Current UUID/name inventory is insufficient for complete migration/export. Import into a managed resource is a separate operation. |
| Device naming and other persistent settings exposed through actions | Documented setters but unimplemented declarative model | Verified readable desired state, platform/async command completion, drift/no-op semantics and safe restoration; do not confuse a sent command with converged configuration. |
| Safe Blueprint/ADE remote deletion | Provider policy/API atomicity gap | Conditional deletion or agreed retention policy with race/dependency semantics. Do not bypass the existing guard. |
| Remediation clearing, ADE clearing/override and detached-upload cleanup | Unresolved contract/observability gaps | See [contract questions](API_CONTRACT_GAPS.md); do not guess nulls, replacement or hidden endpoints. |

[Assignment Map UI documentation](https://support.kandji.io/kb/getting-started-with-assignment-maps) establishes UI capabilities, not a provider API contract. [In-House requirements](https://support.kandji.io/kb/configuring-the-in-house-app-library-item) require reviewed legitimate IPA/update fixtures. Unused features remain required.

## Priority and next bounded work

1. Validate the new classic-root assignment primitive live on an approved two dedicated **existing empty classic Blueprints**, with no devices/enrollment and two owned inactive test items. Recheck assign/remove/read permissions; current fixture and scope are not approved. Bundle exact fixture IDs, complete Blueprint/device/membership emptiness reads, create/adopt/import/no-op, independent detach/drift/repair, changes of both Blueprint and Library Item selectors, owned detach, parent/unrelated-member retention, cleanup and exact-run reconciliation in one reviewed dispatch change. No Blueprint deletion, active policy, device assignment or new lock unlock.
2. With current App infrastructure, source-installer replacement is the next feasible untested App transition: two reviewed README-only PKG versions/hashes, inactive/unassigned record, explicit upload destinations, no reupload for metadata-only operations, import/no-op, cleanup and reconciliation that recognize both artifact versions. That complete dispatch/recovery bundle must be prepared before approval; a one-artifact reconciliation cannot be reused blindly.
3. Obtain legitimate signed IPA/iOS/tvOS fixtures, a dedicated zero-device ABM integration fixture and a configured isolated Routing target; then request only exact permissions/consequences. Existing-token rotation remains forbidden.
4. Resolve missing vendor contracts and managed-import/default/clear semantics before claiming full migration coverage. Extend field, error, replacement and restoration acceptance; do not repeat passed baselines merely to increase counts.

No new live object, permission, unlock, protected-main merge, public merge or release is authorized by this audit. Production configuration adoption is not approved.
