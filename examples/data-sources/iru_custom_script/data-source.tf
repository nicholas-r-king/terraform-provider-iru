# SPDX-License-Identifier: MPL-2.0

# Synthetic UUID: replace with an existing Custom Script Library Item UUID.
data "iru_custom_script" "selected" {
  library_item_id = "aaaaaaaa-bbbb-4ccc-8ddd-000000000092"
}

output "script_name" {
  value = data.iru_custom_script.selected.name
}
