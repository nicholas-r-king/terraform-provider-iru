# Match the tenant's existing settings before adopting management.
resource "iru_blueprint_routing" "example" {
  enrollment_code   = "123456"
  enrollment_active = false
}
