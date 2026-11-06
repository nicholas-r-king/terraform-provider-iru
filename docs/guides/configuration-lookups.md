---
page_title: "Configuration lookups"
---
<!-- SPDX-License-Identifier: MPL-2.0 -->

# Configuration lookups

Configuration data sources read one object by exact UUID. Use inventory to find
an ID; a name is not an identity. Reads do not activate, assign, install, renew or
delete anything, and do not reconstruct upload binaries or all UI configuration.

Required fields missing from an API response fail rather than becoming invented
false/empty values. Documented optional absent/null fields remain Terraform null;
explicit false, empty text and zero values remain distinct where supported.
Observation does not apply managed-resource write defaults or cross-field rules.

Unknown selectors defer during planning; null/invalid UUIDs cannot reach
transport. A 404, permission failure or malformed response is an error, not a
successful empty result. A failed read does not make cached state current. The
client bounds retries, time, response size and redirects; these single-object
lookups do not follow pagination hints.

Sensitive scripts, XML, installer keys and contact fields remain in Terraform
state and machine-readable plans despite CLI redaction. Protect the backend and
plan files. Outputs exposing sensitive values must be marked sensitive; use
non-sensitive stable identifiers for resource addressing. Diagnostics withhold
response bodies and private values.

A reported installer key, digest, expiry or status is metadata, not independent
proof of file integrity, valid signing credentials, installation or device
compliance. Import/adoption still requires matching resource configuration and
review of its first plan. See the repository's validation status for live coverage.
