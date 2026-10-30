<!-- SPDX-License-Identifier: MPL-2.0 -->

# Preparing an experimental Registry release

The intended distribution is the public Terraform Registry at
`nicholas-r-king/iru`, backed by the public GitHub repository
`nicholas-r-king/terraform-provider-iru`. The repository is currently private;
preparing these files does not authorize exposing its contents or history.
No version has been published. Real Iru tenant compatibility has not been
acceptance-tested. A public development release must retain that qualification.

The manual **Prepare experimental release draft** workflow creates a signed,
experimental **draft** GitHub release only. It never changes repository visibility,
creates a tag, finalizes a release, or registers a provider. Stable tags are
rejected while real tenant validation is incomplete. A draft is not discoverable
by the Registry and cannot be installed from it.

## Owner prerequisites

1. Review and merge the intended provider changes and release preparation through
   the normal protected PR process. Choose the exact source commit; earlier CI
   results do not qualify a different commit. Ensure the workflow and that commit
   contain the same reviewed release configuration/tooling.
   Require the normal **Release configuration** CI job to pass on that source:
   it runs GoReleaser v2.19.0 `check` on the exact tested SHA with read-only
   permissions and no signing secrets. It does not build or publish a release.
2. Complete separately authorized isolated real-Iru validation before publication,
   covering the documented resource lifecycles, imports, no-op plans, refresh,
   permissions and safe cleanup. Record results without tenant data or credentials.
   Synthetic tests cannot establish live compatibility. If the owner deliberately
   distributes an unvalidated experimental preview, that requires a separate
   explicit decision and clear release/documentation disclosure; this preparation
   does not make that decision.
3. Choose an experimental SemVer version, such as `0.1.0-alpha.1`, and add a
   `## 0.1.0-alpha.1` section to `CHANGELOG.md`. Keep the compatibility qualification
   and supported Terraform version explicit. Create its `v`-prefixed tag on the
   reviewed commit through the owner's trusted process. No branch may share its
   name. Do not move a released tag or replace existing release assets.
4. Configure the `registry-release` GitHub environment with required reviewers,
   prevent self-review, and restrict deployment to the trusted `main` workflow.
   Store `GPG_PRIVATE_KEY` and a nonempty `PASSPHRASE` as **environment secrets**,
   and the complete primary signing-key fingerprint as environment variable
   `GPG_FINGERPRINT`. Set environment variable `IRU_RELEASE_SETUP` to
   `reviewers-and-signing-configured` only after reviewing these protections.
   Keep tenant credentials out of this environment. Provision/export a signing
   key through an approved owner process; no key is supplied/generated here.
   The Registry accepts RSA/DSA keys, not ECC. Use a dedicated RSA signing key
   (for example RSA 4096) with signing capability on the primary key. The workflow
   checks the primary key algorithm/capability and forces that exact fingerprint
   for signing, avoiding automatic selection of an incompatible ECC subkey.
5. Only after those controls are ready, explicitly enable manual preparation by
   setting repository variable `IRU_DRAFT_RELEASE_ENABLED` to `true`. Absent that
   variable the workflow skips all jobs; absent environment setup, fingerprint,
   key or passphrase the signing job fails before any release API write.
6. Dispatch the workflow from `main` with the existing tag and its full lowercase
   40-character commit SHA. Review all native results and generated legal/source
   material before approving the signing environment. Approval authorizes creation
   of a draft, not public publication.

## What the workflow checks

All provider tests run without tenant secrets, on the requested source commit:
Linux amd64 with Go 1.25.0 and 1.27.3, Linux arm64, macOS amd64, and macOS arm64
with Go 1.27.3. Terraform 1.17.0 is pinned. Checks include build, module integrity,
formatting, vet, unit/Framework tests, mock Terraform CLI lifecycles, example
formatting, and generated documentation. Windows is not a release target.

GoReleaser v2.19.0 is explicitly bound to the requested tag, including when other
tags point at the same commit. It builds four CGO-disabled binaries with trimmed
paths, a pinned Go toolchain, no VCS-path stamp, and the release version in
`main.version`.
Archive member times/modes are fixed. The workflow builds twice and compares the
complete checksum lists; failure prevents signing. Its own release publishing pipe
is disabled; only the gated workflow job can create the GitHub draft. This checks
reproducibility within that runner/toolchain, not independently reproduced builds.

Each ZIP includes `LICENSE` and a generated `release-legal/` tree containing the
exact provider source-commit links, dependency module/version source locations,
and license/copyright/NOTICE/PATENTS/AUTHORS files discovered throughout the
linked dependency modules and Go runtime source tree. Dependency discovery is
the union of the four release targets, excludes test/tool-only dependencies,
and rejects module replacements or missing legal material. Automated filename
collection is a review aid: review dependency obligations, nested notices and
source accessibility before distribution. Preserve required additional material
if any dependency changes. Update the pinned Framework legal-file assertion when
updating that dependency.
Source-code extensions such as `.go` are excluded even when their names begin
with `copyright` or `license`. The generated tree also contains a `go.mod`
boundary so normal `go test ./...`, `go vet ./...` and package discovery do not
traverse it if reviewed source material is retained in the future. Git ignore
rules alone do not provide that Go package boundary.

The release verifier checks all five SHA256 entries, protocol 6.0 manifest,
safe ZIP paths, required legal/source material, exact source notice, provider
binary name, embedded version, target architecture and build settings. Each
target's native runner then installs its ZIP through an offline filesystem mirror
and exports the provider schema using Terraform. These are package/protocol smoke
checks with synthetic configuration; they do not contact Iru.

Only the final environment-gated job receives the signing secrets. It imports the
key into an ephemeral GnuPG home, signs the checked checksum file with a binary
detached OpenPGP signature, verifies that signature, and creates a new draft
prerelease. It refuses to replace an existing release. No source checkout or
provider code runs in the signing job. GnuPG state is removed afterward.

## Artifact contract

For version `0.1.0-alpha.1`, the draft contains:

- Four `terraform-provider-iru_0.1.0-alpha.1_{linux,darwin}_{amd64,arm64}.zip`
  files, each containing the root binary
  `terraform-provider-iru_v0.1.0-alpha.1` and legal material.
- `terraform-provider-iru_0.1.0-alpha.1_manifest.json`, declaring protocol `6.0`.
- `terraform-provider-iru_0.1.0-alpha.1_SHA256SUMS`, covering all four ZIPs and
  the manifest.
- `terraform-provider-iru_0.1.0-alpha.1_SHA256SUMS.sig`, a binary detached GPG
  signature. An ASCII-armored signature does not satisfy the packaging contract.

Safe local preparation commands, from a clean tagged checkout with the pinned
Go toolchain and GoReleaser installed, are:

```sh
go run ./tools/release identity -tag v0.1.0-alpha.1 -commit FULL_COMMIT_SHA
go run ./tools/release materials -tag v0.1.0-alpha.1 -commit FULL_COMMIT_SHA
goreleaser check
goreleaser release --clean --skip=sign,publish
```

For manual verification, copy `terraform-registry-manifest.json` into `dist/`
using its versioned manifest name, then run `go run ./tools/release verify` with
the same `-tag` and `-commit`. Native Linux/macOS can also run the `smoke` command
with those flags and `-terraform /path/to/terraform`. These commands cannot sign
or publish. `dist/` and `release-legal/` are ignored generated files; start with a
fresh checkout when rebuilding legal material.

## Separate public publication decision

Before finalizing the first public release, the owner must explicitly approve
making the GitHub repository and intended source history public after reviewing
them for secrets, confidential research, licensing and provenance. Preserve the
exact tagged source and dependency source availability for MPL distribution.
Review the generated draft assets, changelog, documentation and compatibility
statement; verify the checksum signature with the public key independently.

Sign in to the Terraform Registry with the authorized GitHub account, ensure the
namespace/repository access is correct, and add the release signing **public** key
to the Registry. The private key/passphrase never goes to the Registry. Follow
the current Registry UI to register the provider and configure its GitHub
integration. Finalize the reviewed GitHub release only under separate owner
authorization, confirm Registry ingestion, then verify `terraform init` and schema
loading from `nicholas-r-king/iru` on the supported platforms. A preview version
must be selected explicitly. Update the README installation claim only after
that succeeds. Later changes require a new immutable version.

Recheck the primary references before enabling publication:

- [HashiCorp provider publishing requirements](https://developer.hashicorp.com/terraform/registry/providers/publishing)
- [HashiCorp Framework scaffolding release configuration](https://github.com/hashicorp/terraform-provider-scaffolding-framework/blob/main/.goreleaser.yml)
- [GoReleaser archive configuration](https://goreleaser.com/customization/package/archives/)
- [GoReleaser checksums](https://goreleaser.com/customization/package/checksum/)
- [GoReleaser draft releases](https://goreleaser.com/customization/publish/scm/)
