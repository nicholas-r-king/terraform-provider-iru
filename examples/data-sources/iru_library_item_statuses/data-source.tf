data "iru_library_item_statuses" "example" {
  library_item_id = "00000000-0000-4000-8000-000000000001"
}

output "reported_status_count" {
  value = data.iru_library_item_statuses.example.record_count
}

output "reported_statuses" {
  value     = data.iru_library_item_statuses.example.statuses
  sensitive = true
}
