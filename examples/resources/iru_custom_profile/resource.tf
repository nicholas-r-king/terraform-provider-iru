# SPDX-License-Identifier: MPL-2.0

# Synthetic XML only; replace with a validated profile before real use.
resource "iru_custom_profile" "example" {
  name        = "Synthetic profile example"
  profile     = "<plist version=\"1.0\"><dict><key>PayloadDisplayName</key><string>Synthetic example</string></dict></plist>"
  active      = false
  runs_on_mac = true
}
