<!-- SPDX-License-Identifier: MPL-2.0 -->

# Releasing

The public provider is `nicholas-r-king/iru`; preview `0.1.0-alpha.1` is already
published. Development changes require a new version and separate release
approval. Never move a released tag or replace its assets.

## Prepare and verify

1. Review the exact source commit, changelog and compatibility statement. Require
   its native test, release-configuration and package-rehearsal checks to pass.
   Record separately approved live validation and disclose remaining limits from
   [validation status](VALIDATION_STATUS.md).
2. Select a SemVer version and create its `v`-prefixed tag through the owner's
   release process. The tag must identify the reviewed commit; a branch must not
   share its name. Stable releases remain inappropriate while required acceptance
   is incomplete.
3. Build Darwin/Linux amd64/arm64 packages from a clean tagged checkout with the
   pinned toolchain and GoReleaser configuration. The repository's release tools
   verify identity, package contents, manifest, architecture and legal material.
4. Review dependency licenses/notices and source availability. Each ZIP includes
   `LICENSE` and `release-legal/`; automatic filename collection does not settle
   every dependency's obligations. Compare repeated builds, then run each package
   natively through offline mirror installation and schema export.

With `IRU_RELEASE_TAG` and `IRU_RELEASE_COMMIT` already set to the reviewed tag and
full source SHA, local preparation is:

```sh
go run ./tools/release identity -tag "$IRU_RELEASE_TAG" -commit "$IRU_RELEASE_COMMIT"
go run ./tools/release materials -tag "$IRU_RELEASE_TAG" -commit "$IRU_RELEASE_COMMIT"
goreleaser check
goreleaser release --clean --skip=sign,publish
```

These commands do not sign or publish. `dist/` and `release-legal/` are ignored
outputs. See `go run ./tools/release` for `verify`/`smoke` flags; pass the same
reviewed tag/commit and a verified native Terraform executable.

## Artifacts and signing

A release contains four `terraform-provider-iru_VERSION_OS_ARCH.zip` packages,
`terraform-provider-iru_VERSION_manifest.json` declaring protocol `6.0`,
`terraform-provider-iru_VERSION_SHA256SUMS`, and its binary detached GPG signature
`terraform-provider-iru_VERSION_SHA256SUMS.sig`. ZIPs contain the versioned provider
binary and required legal material. Checksums cover the packages and manifest.

Local signing is supported. Use the owner's approved key custody, verify the
signature independently with its public key, and keep private keys/passphrases
out of source, logs, command arguments and release assets. No private-key upload,
key creation or rotation is implied by preparing a release.

## Optional GitHub draft workflow

The existing **Prepare experimental release draft** workflow is a separate
signing path. Its controls are repository policy, not Registry requirements:

- `IRU_DRAFT_RELEASE_ENABLED=true` enables dispatch. Otherwise it stays off.
- The `registry-release` environment requires reviewers, prevents self-review and
  restricts deployment to the trusted `main` workflow.
- That environment supplies `GPG_PRIVATE_KEY`, `PASSPHRASE`, `GPG_FINGERPRINT`, and
  `IRU_RELEASE_SETUP=reviewers-and-signing-configured`. Provisioning these secrets
  requires a separately approved owner action; tenant credentials do not belong
  in the environment. The workflow enforces its supported signing-key algorithm.
- Dispatch from `main` with the existing tag and full commit SHA. The workflow
  checks/tests the source, builds twice, compares checksums, verifies legal
  material and runs four native package smoke jobs before the signing gate.
- Only the final gated job receives signing secrets. It signs/verifies in an
  ephemeral GnuPG home and creates a new experimental draft prerelease. It does
  not create tags, change visibility, finalize releases or publish to the Registry.

Do not weaken these controls to make preparation pass. A draft is not a public
Registry release, and passing package checks is not live tenant acceptance.

## Publish and verify

Follow [HashiCorp's current publishing instructions](https://developer.hashicorp.com/terraform/registry/providers/publishing).
An individual GitHub account can publish; an HCP organization is optional
([account guidance](https://developer.hashicorp.com/terraform/registry#user-account)).
Verify the existing namespace, repository integration and public signing key
before registering anything again. Only the public key belongs in the Registry.

After owner approval, finalize the reviewed GitHub release and confirm Registry
ingestion. In a clean workspace without development overrides, explicitly select
the preview version, run `terraform init`, check the signing identity and export
its schema. Record the actual result before changing installation claims. Do not
reuse an old release's approval for a new version or permission change.
