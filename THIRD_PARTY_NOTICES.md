<!-- SPDX-License-Identifier: MPL-2.0 -->

# Third-party provenance and license inventory

Original project source is licensed under [MPL-2.0](LICENSE). This does not
relicense any third-party component. The implementation and synthetic fixtures
were written for this repository with AI-assisted development; no vendor or
community-provider source files were copied or vendored. Public API facts and
references are recorded in [the API contract](docs/api-contract.md).

The following versions are pinned in go.mod and were independently confirmed in
the validated executable's Go build information. License files were inspected
from the checksum-verified Go module cache. Upstream copyright and license
notices remain with those separately fetched modules.

| Module | Version | Upstream license |
| --- | --- | --- |
| github.com/fatih/color | v1.18.0 | MIT |
| github.com/golang/protobuf | v1.5.4 | BSD-3-Clause |
| github.com/hashicorp/go-hclog | v1.6.3 | MIT |
| github.com/hashicorp/go-plugin | v1.7.0 | MPL-2.0 |
| github.com/hashicorp/go-uuid | v1.0.3 | MPL-2.0 |
| github.com/hashicorp/terraform-plugin-framework | v1.19.0 | MPL-2.0 |
| github.com/hashicorp/terraform-plugin-go | v0.31.0 | MPL-2.0 |
| github.com/hashicorp/terraform-plugin-log | v0.10.0 | MPL-2.0 |
| github.com/hashicorp/terraform-registry-address | v0.4.0 | MPL-2.0 |
| github.com/hashicorp/terraform-svchost | v0.1.1 | MPL-2.0 |
| github.com/hashicorp/yamux | v0.1.2 | MPL-2.0 |
| github.com/mattn/go-colorable | v0.1.14 | MIT |
| github.com/mattn/go-isatty | v0.0.20 | MIT |
| github.com/mitchellh/go-testing-interface | v1.14.1 | MIT |
| github.com/oklog/run | v1.1.0 | Apache-2.0 |
| github.com/vmihailenco/msgpack/v5 | v5.4.1 | BSD-2-Clause |
| github.com/vmihailenco/tagparser/v2 | v2.0.0 | BSD-2-Clause |
| golang.org/x/net | v0.48.0 | BSD-3-Clause |
| golang.org/x/sys | v0.39.0 | BSD-3-Clause |
| golang.org/x/text | v0.32.0 | BSD-3-Clause |
| google.golang.org/genproto/googleapis/rpc | v0.0.0-20251202230838-ff82c1b0f217 | Apache-2.0 |
| google.golang.org/grpc | v1.79.2 | Apache-2.0 |
| google.golang.org/protobuf | v1.36.11 | BSD-3-Clause |

Go's standard library has its own BSD-style license and third-party notices.
Additional test/build metadata and historical module checksums may appear in
go.sum; checksum entries are not vendored source or a redistribution of modules.
The external Go toolchain and Terraform CLI used for validation are not bundled
with this source repository. Their licenses are separate from the provider's.

## Source-only publication

This repository contains no vendored module code, installers, tool archives, or
compiled provider binaries. Dependencies are fetched by Go using go.mod/go.sum.
The official, unmodified project LICENSE was obtained from
https://www.mozilla.org/media/MPL/2.0/index.txt; its SHA256 is
`3f3d9e0024b1921b067d6f7f88deb4a60cbe7a78e76c64e3f1d7fc3b779b9d04`.
Original Go and Terraform example files carry SPDX MPL-2.0 notices.

## Before distributing a binary or vendored source

Retain and supply the applicable upstream license texts, copyright notices,
disclaimers, and required NOTICE material for the actual components shipped.
The grpc module includes NOTICE.txt. Any additional dependencies included by a
later build must also be inventoried; test tooling can have its own notices.
For MPL-covered code in an executable, inform recipients where its corresponding
source and MPL terms can be obtained. Make applicable covered modifications
available under MPL. This inventory is not a substitute for packaging the full
notices required by the versions actually distributed.

Authoritative references: [Mozilla's MPL FAQ](https://www.mozilla.org/en-US/MPL/2.0/FAQ/),
[Framework v1.19.0 LICENSE](https://github.com/hashicorp/terraform-plugin-framework/blob/v1.19.0/LICENSE),
and [Plugin Go v0.31.0 LICENSE](https://github.com/hashicorp/terraform-plugin-go/blob/v0.31.0/LICENSE).
