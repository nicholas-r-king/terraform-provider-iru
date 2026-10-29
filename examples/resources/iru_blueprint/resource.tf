resource "iru_blueprint" "example" {
  name              = "Example Blueprint"
  type              = "map"
  description       = "Managed with Terraform"
  enrollment_active = false
}
