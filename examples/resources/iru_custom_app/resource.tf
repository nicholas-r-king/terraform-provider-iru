# SPDX-License-Identifier: MPL-2.0

# Synthetic key: replace with a completed upload's actual file_key.
resource "iru_custom_app" "example" {
  name                = "Synthetic app example"
  file_key            = "companies/companies/11111111-2222-4333-8444-555555555555/library/custom_apps/synthetic.pkg"
  install_type        = "package"
  install_enforcement = "install_once"
  active              = false
  restart             = false
}
