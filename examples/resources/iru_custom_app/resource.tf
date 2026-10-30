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

# Alternative: upload a local installer. Replace the path with your package.
resource "iru_custom_app" "local_installer" {
  name          = "Local installer example"
  source_file   = "./installers/example.pkg"
  source_sha256 = filesha256("./installers/example.pkg")
  install_type  = "package"
  active        = false
}
