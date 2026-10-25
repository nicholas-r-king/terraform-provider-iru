# SPDX-License-Identifier: MPL-2.0

terraform {
  required_providers {
    iru = {
      source = "nicholas-r-king/iru"
    }
  }
}

# Prefer IRU_API_URL and IRU_API_TOKEN from a secure environment.
# The provider currently requires a local development override.
provider "iru" {}
