# SPDX-License-Identifier: MPL-2.0

# Synthetic UUID: replace with an existing Custom App Library Item UUID.
data "iru_custom_app" "selected" {
  library_item_id = "aaaaaaaa-bbbb-4ccc-8ddd-000000000094"
}

output "app_name" {
  value = data.iru_custom_app.selected.name
}
