<!-- SPDX-License-Identifier: MPL-2.0 -->

# API contract and provenance

Checked on 2026-10-25. All requests made during research fetched public
documentation or public source. No tenant API request was made.

## Official sources

- [Iru Endpoint Management API](https://api-docs.iru.com/), Custom Scripts folder.
  Its HTML points to the public Postman collection
  `https://api-docs.iru.com/api/collections/15284493/TzCTZkBe?segregateAuth=true&versionTag=latest`.
  Retrieved collection SHA256:
  `1e35675566b2066b3f786ac9f8eb156b65dbfe27882e11f7c43bc006701a2921`.
  Request/response examples establish this wire contract:

  | Operation | Method/path | Expected success |
  | --- | --- | --- |
  | Create | POST `/api/v1/library/custom-scripts` | 201 and script JSON |
  | Read | GET `/api/v1/library/custom-scripts/{library_item_id}` | 200 and script JSON |
  | Update | PATCH `/api/v1/library/custom-scripts/{library_item_id}` | 200 and script JSON |
  | Delete | DELETE `/api/v1/library/custom-scripts/{library_item_id}` | 204, no body |

  All use bearer authentication. Core response fields are `id`, `name`, `active`,
  `execution_frequency`, `restart`, `script`, and `remediation_script`. The
  collection also documents `show_in_self_service`, `self_service_category_id`,
  and `self_service_recommended` as request fields. Timestamps are not managed.
  No endpoint paths, methods, or field names were taken from an undocumented UI.

- [Vendor iructl source](https://github.com/kandji-inc/iructl), examined at commit
  [`f918fbfba8582f4c38e880f1781314ec949e6505`](https://github.com/kandji-inc/iructl/tree/f918fbfba8582f4c38e880f1781314ec949e6505).
  `src/iructl/api/scripts.py` corroborates the methods, JSON requests, and four
  execution-frequency values. `payload.py` documents optional Self Service
  response metadata. `client.py` corroborates Iru/Kandji US/EU API origins.
  The repository identifies an MIT license. No Python code was copied.
- [iructl API Custom Scripts](https://github.com/kandji-inc/iructl/wiki/API-Custom-Scripts)
  documents the constraints that `no_enforcement` requires Self Service and that
  Self Service requires a category UUID. These are validated before writes.
- [HashiCorp Framework v1.19.0 release](https://github.com/hashicorp/terraform-plugin-framework/releases/tag/v1.19.0)
  was the latest release URL returned during research. Its module requires Go
  1.25.0 and terraform-plugin-go v0.31.0. Source inspected at commit
  `c7ac25e86333d194946fb5e3fd1114e7d101fc23` to verify interface signatures.
- HashiCorp's [resource import](https://developer.hashicorp.com/terraform/plugin/framework/resources/import)
  and [read](https://developer.hashicorp.com/terraform/plugin/framework/resources/read)
  documentation inform UUID import followed by refresh, and 404 removal.

## Deliberate limits and unresolved behavior

- This milestone implements only Custom Scripts. It does not deploy or assign
  scripts to Blueprints/Assignment Maps, and does not manage devices or profiles.
- Older official GET examples omit Self Service metadata even with
  `no_enforcement`. The vendor CLI's current validation requires that metadata.
  The provider rejects this ambiguous legacy combination instead of guessing.
- The provider adds a constraint that category/recommendation are configured
  only when Self Service is enabled. The API does not document how to clear
  hidden category metadata; the provider does not send undocumented null clears.
- Optional remote Self Service flags omitted from a response default to false,
  consistent with the vendor payload model. A visible Self Service script without
  a valid category is rejected.
- No maximum name/script lengths or script interpreter requirements were
  invented. JSON types, nonempty name/audit content, documented enums,
  dependencies, UUIDs, and safe provider origins are validated.
- Retry and timeout bounds are provider policy, not a claim about Iru SLAs or
  idempotency. No documented idempotency key was found, so writes are not retried.
- Mocked tests can establish protocol behavior against these documented shapes;
  they cannot establish tenant-side permissions, normalization, remediation
  clearing, propagation timing, or future API changes.

## Community options reviewed

| Option | Observed scope | Stated license | Decision |
| --- | --- | --- | --- |
| [dmcphearson/terraform-provider-iru](https://github.com/dmcphearson/terraform-provider-iru) | Broad provider including Custom Scripts, import, assignments and apps | MPL-2.0 | Existing option worth evaluating separately; no code reused |
| [MScottBlake/terraform-provider-iru](https://github.com/MScottBlake/terraform-provider-iru) | Provider with device/import/action examples | MPL-2.0 | Broader scope than this milestone; no code reused |

A small original implementation keeps this milestone limited to the verified
contract and makes review of transport, state, and error behavior practical.
These observations do not certify either community provider's correctness or
production readiness. License statements come from their repository pages.

## License provenance

Original project source is licensed under [MPL-2.0](../LICENSE), selected by the
repository owner. The implementation and synthetic fixtures were written for
this repository with AI-assisted development, using the public references above.
No vendor or community-provider source files were copied or vendored. The pinned HashiCorp Framework and Plugin Go
dependencies carry MPL-2.0 source notices; Go's standard library is distributed
under its own BSD-style terms. Transitive modules are recorded in `go.mod` and
`go.sum` and must retain their own licenses if later distributed or vendored.
See [third-party notices](../THIRD_PARTY_NOTICES.md) for the audited dependency
inventory and notice requirements for any future binary distribution.
