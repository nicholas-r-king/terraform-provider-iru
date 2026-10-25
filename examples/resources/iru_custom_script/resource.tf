# SPDX-License-Identifier: MPL-2.0

resource "iru_custom_script" "example" {
  name                = "Synthetic audit example"
  script              = "#!/bin/sh\nexit 0\n"
  execution_frequency = "once"
  active              = false
  restart             = false
}
