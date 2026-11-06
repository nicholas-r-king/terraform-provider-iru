# SPDX-License-Identifier: MPL-2.0

terraform {
  required_providers {
    iru = {
      source  = "nicholas-r-king/iru"
      version = "= 0.2.0-alpha.1"
    }
  }
}

# Prefer IRU_API_URL and IRU_API_TOKEN from a secure environment.
# Use a local development override only for unreleased source changes.
provider "iru" {}
