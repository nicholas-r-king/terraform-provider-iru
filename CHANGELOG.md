<!-- SPDX-License-Identifier: MPL-2.0 -->

# Changelog

## Unreleased

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

No Registry version or binary release has been published.
