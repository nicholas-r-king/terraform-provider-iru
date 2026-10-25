# SPDX-License-Identifier: MPL-2.0

# Synthetic UUIDs: replace with an existing Blueprint and one of its Library Items.
data "iru_library_item" "selected" {
  blueprint_id    = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
  library_item_id = "11111111-2222-4333-8444-555555555555"
}

output "library_item_name" {
  value = data.iru_library_item.selected.name
}
