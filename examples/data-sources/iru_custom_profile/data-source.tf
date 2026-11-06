# SPDX-License-Identifier: MPL-2.0

# Synthetic UUID: replace with an existing Custom Profile Library Item UUID.
data "iru_custom_profile" "selected" {
  library_item_id = "aaaaaaaa-bbbb-4ccc-8ddd-000000000091"
}

output "profile_name" {
  value = data.iru_custom_profile.selected.name
}
