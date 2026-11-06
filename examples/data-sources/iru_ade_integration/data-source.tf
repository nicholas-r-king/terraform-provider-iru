# SPDX-License-Identifier: MPL-2.0

# Synthetic UUID: replace with an existing ADE integration record UUID.
data "iru_ade_integration" "selected" {
  integration_id = "aaaaaaaa-bbbb-4ccc-8ddd-000000000096"
}

output "ade_server_name" {
  value = data.iru_ade_integration.selected.server_name
}
