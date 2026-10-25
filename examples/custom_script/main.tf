# SPDX-License-Identifier: MPL-2.0

terraform {
  required_providers {
    iru = {
      source = "nicholas-r-king/iru"
    }
  }
}

# Read IRU_API_URL and IRU_API_TOKEN from your existing secure environment.
# This provider is unpublished; use a reviewed local development override.
provider "iru" {}

resource "iru_custom_script" "example" {
  name                = "Synthetic audit example"
  script              = "#!/bin/sh\nexit 0\n"
  execution_frequency = "once"
  active              = false
  restart             = false
}
