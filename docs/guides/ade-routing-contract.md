---
page_title: "ADE Blueprint and routing selection"
---
<!-- SPDX-License-Identifier: MPL-2.0 -->

# ADE Blueprint and routing selection

An ADE integration selects either a fixed Blueprint UUID or Blueprint Routing.
Configure `blueprint_id` with routing false, or omit it and set
`use_blueprint_routing = true`. An empty configured string is not an omitted UUID.

The getter accepts the observed legacy `blueprint.id` form for a fixed Blueprint,
or explicit `blueprint_id`/`use_blueprint_routing` fields. Routing requires an
explicit true flag and null Blueprint. Contradictory assignments, missing selection
and malformed UUIDs fail instead of being guessed from defaults.

Metadata PATCH uses JSON: routing sends `blueprint_id=null` together with the true
flag. Create/token renewal uses multipart fields, where an empty `blueprint_id`
and explicit routing flag represent that selection. The public API describes
routing assignment, but live acceptance of this multipart form remains incomplete.
Use an isolated integration for lifecycle validation; do not rotate an existing
production token to manufacture a fixture.

Configure existing assignment/contact values before import. The read-only
integration lookup observes metadata but neither adopts nor renews it. A local
`server_token_version` is a renewal trigger, not remote token identity. Token
input is write-only; a lost renewal response still needs explicit reconciliation.

The integration resource refuses remote deletion because associated device
records can be unassigned. Retention also needs `prevent_destroy` for tainted
create-before-destroy paths. Device-level ADE overrides and Blueprint Routing
rules are separate concepts; this integration does not manage routing rules.

Reference: [Iru API](https://api-docs.iru.com/).
