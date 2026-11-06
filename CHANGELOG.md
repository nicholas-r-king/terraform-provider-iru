<!-- SPDX-License-Identifier: MPL-2.0 -->

# Changelog

## Unreleased

## 0.2.0-alpha.1

- Add exact-UUID Script/Profile/App/In-house/ADE configuration lookups and Library Item status snapshots.
- Add guarded ADE tag settings and preserve legacy numeric user observations without enabling numeric assignments.
- Block Blueprint/ADE destruction and observable replacements; tainted create-first replacements still require `prevent_destroy`.
- Refuse current Assignment Map placement and new classic Blueprint creation; graph/routing-rule/built-in parameter contracts remain unresolved.
- Retain state after partial failures and incomplete reads, preserve meaningful script/XML content, and validate known inputs independently of unknown expressions.
- Reject unsupported Script remediation clearing; no alternate removal contract is inferred.
- Protect installer hashing/cancellation, redact installer/contact fields and consolidate documentation.
- Publish signed prereleases from explicit version tags after native CI and environment approval; verify Registry installation on all four platforms.

### Upgrading from 0.1.0-alpha.1

Inspect the first plan before applying. Leading script whitespace is meaningful
drift; match returned content when importing. Installer keys and ADE contact
fields are now sensitive, so derived outputs must also be sensitive and cannot
be used directly as instance keys.

New Blueprint containers default to Map; observed imported types are retained.
Classic creation and Blueprint/ADE integration deletion are refused. Configure
`prevent_destroy` for retained objects, including protection against tainted
create-before-destroy paths. Current Map placement and other unresolved API
contracts remain outside implemented coverage. Full live acceptance is incomplete;
see [validation status](VALIDATION_STATUS.md).

## 0.1.0-alpha.1

Experimental preview. Real Iru tenant compatibility has not been acceptance-tested.
Synthetic tests and native package installation/schema checks cover Linux and macOS
on amd64/arm64 with Terraform 1.17.0. Windows is not a release target.

- Public Registry release preparation: default-off manual experimental draft workflow, native package gates, signed checksums and source/license verification; no version or public visibility change is implied.
- ADE integration Blueprint Routing support and read-only Blueprint-template/ADE public-key bootstrap helpers.
- Avoid resending unchanged optional Script/App fields during PATCH; omit absent optional App content on creation while retaining explicit changed-content clearing.
- Existing Device/ADE-device settings resources and exact-UUID metadata lookups using selective GET/PATCH, documented clearing, routing coupling, and management release without device deletion.
- Directory-user UUID identity/status lookup with nullable reported fields and no deprecated IDs or directory internals.
- Blueprint routing singleton enrollment settings and sensitive lookup using the documented GET/PATCH contract; destroy releases management without changing tenant settings.
- Streaming Custom App and In-House App installer uploads from planned SHA256-verified local files, with token separation and bounded IPA status validation.
- Installer content drift repair, metadata-only upload avoidance, and import adoption of existing installer keys.
- Blueprint metadata/enrollment and Tag CRUD/import resources with bounded refresh.
- In-House App CRUD/import with existing validated IPA keys and selective installer replacement.
- ADE integration CRUD/import and Apple token renewal with write-only token input.
- Device Note CRUD with device/Note composite import; no device lifecycle operations.
- Configuration inventory UUID/name projection for eight documented object families.
- Omit an absent remediation script on creation while retaining explicit removal on update.
- Allowlisted write-validation field names and explicit error codes for HTTP
  400/422, with bounded parsing and no response messages or values in errors.
- UUID-only Blueprint and Blueprint-scoped Library Item metadata data sources
  with allowlisted outputs and bounded, origin-checked pagination.
- Custom Profile multipart content and metadata CRUD/import with explicit target
  platforms, sensitive XML and no arbitrary whitespace normalization.
- Custom App Library Item CRUD/import using pre-uploaded installer keys, with
  optional server-reported hash/size and metadata-only update handling.
- Initial Framework provider and Custom Scripts CRUD/import with synthetic tests.
- Strict API origins, redirect refusal, bounded read retries, redacted errors,
  stable UUID state and explicit import-default behavior.
- MPL-2.0 source licensing.
- Versioned User-Agent, generated reference docs,
  standard examples and repeatable manual checks.
- Portable Go contributor checks and documentation generation, with Linux/macOS
  as primary targets and no PowerShell requirement.
- Secret-free native Linux/macOS amd64/arm64 CI for builds, unit/Framework and
  mock Terraform lifecycles, vet, examples and deterministic schema-based docs.
