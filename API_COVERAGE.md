<!-- SPDX-License-Identifier: MPL-2.0 -->

# API coverage

Contracts are based on the [official Iru API reference](https://api-docs.iru.com/).
The generated reference describes the supported fields. UI features do not imply
a complete public read/write contract.

| Resource | Supported behavior | Important limit |
| --- | --- | --- |
| `iru_custom_script` | Create/read/update/import/delete | Clearing nonempty remediation is blocked after an API rejection; Self Service needs a category. |
| `iru_custom_profile` | Unsigned XML configuration and platform flags | Syntax validation is not Apple payload validation; signed/binary profiles are unsupported. |
| `iru_custom_app` | Existing installer key or local package/ZIP/image source, metadata and scripts | Local uploads are capped at 5 GiB. Larger uploads, Liftoff timing and detached-file cleanup need contracts. |
| `iru_in_house_app` | Existing IPA key or upload/validation workflow | No signed-IPA live lifecycle validation. |
| `iru_tag` | Tag CRUD; exact UUID lookup through bounded listing | Device associations are separate. |
| `iru_device_note` | CRUD on an existing enrolled device | Device UUID plus Note UUID identifies an import; this does not manage the device. |
| `iru_blueprint` | Map container creation/cloning, metadata and enrollment settings | No graph/placement management; classic creation and remote deletion are refused. |
| `iru_ade_integration` | Metadata, default Blueprint/routing and write-only token renewal | Local token version is not remote token identity. Deletion is refused. |
| `iru_device_settings` | Configured Blueprint/user/asset/tag fields on an existing device | Omitted fields stay unmanaged; destroy releases management. |
| `iru_ade_device_settings` | Configured ADE assignment, routing/override and full tag list | Tags/override require observable getters. Numeric legacy users are read-only; user/asset clearing is not inferred. |
| `iru_blueprint_routing` | Existing tenant-wide enrollment code/activation | No rule management or remote DELETE. |
| `iru_blueprint_library_item_assignment` | Deprecated classic-root membership only | All current Assignment Maps are refused until node placement can be read reliably. |

## Read-only data sources

Configuration lookups: `iru_custom_script`, `iru_custom_profile`, `iru_custom_app`,
`iru_in_house_app` and `iru_ade_integration` use exact UUIDs. They omit signed URLs,
installer bytes and write-only Apple tokens. Sensitive configuration remains in state.

Identity/metadata lookups: `iru_blueprint`, `iru_library_item`, `iru_device`,
`iru_ade_device` and `iru_user`. `iru_inventory` lists eight configuration families;
`iru_blueprint_templates` and `iru_ade_public_key` support enrollment setup.
`iru_blueprint_routing` reads enrollment settings. `iru_library_item_statuses`
returns a paginated sensitive status snapshot, not placement or installation proof.

## Unresolved contracts

- Map node/edge/condition CRUD, stable item-to-node reads, multiple placements and
  canonical node removal. Aggregate membership is insufficient for import/drift.
- Routing criteria, priority and fallback rules; built-in/Auto App parameters and
  complete configuration export; Self Service category CRUD/ordering.
- Atomic safe Blueprint/ADE deletion. A client-side empty-device check can race
  an assignment; tainted create-first replacements also need `prevent_destroy`.
- ADE user/asset clearing and omitted override metadata; safe renewal reconciliation
  after a lost response; Script remediation clearing.
- Custom App upload readiness, files above 5 GiB, Liftoff fields and detached upload
  cleanup. The five-second upload grace is not a readiness guarantee.

No undocumented routes, identity mapping or destructive fallbacks are inferred.
See [validation status](VALIDATION_STATUS.md) for what has actually been tested.
