# SPDX-License-Identifier: MPL-2.0

# Synthetic UUID: replace with an existing Blueprint ID.
data "iru_blueprint" "selected" {
  blueprint_id = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
}

output "blueprint_name" {
  value = data.iru_blueprint.selected.name
}
