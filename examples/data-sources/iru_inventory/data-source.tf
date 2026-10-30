data "iru_inventory" "scripts" {
  kind = "custom_scripts"
}

data "iru_inventory" "categories" {
  kind = "self_service_categories"
}
