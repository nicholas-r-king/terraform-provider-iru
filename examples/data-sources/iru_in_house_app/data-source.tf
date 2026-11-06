# SPDX-License-Identifier: MPL-2.0

# Synthetic UUID: replace with an existing In-House App Library Item UUID.
data "iru_in_house_app" "selected" {
  library_item_id = "aaaaaaaa-bbbb-4ccc-8ddd-000000000095"
}

output "in_house_app_name" {
  value = data.iru_in_house_app.selected.name
}
