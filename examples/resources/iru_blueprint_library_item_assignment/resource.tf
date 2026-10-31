# Review policy effects before assigning to a Blueprint with enrolled devices.
resource "iru_blueprint_library_item_assignment" "example" {
  blueprint_id    = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
  library_item_id = "11111111-2222-4333-8444-555555555555"
}
