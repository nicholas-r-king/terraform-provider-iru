<!-- SPDX-License-Identifier: MPL-2.0 -->

# Releasing

The public provider is `nicholas-r-king/iru`. The **Release provider** workflow
runs when an owner pushes an explicit `v`-prefixed prerelease tag. It never creates
tags, changes settings, rotates keys or replaces an existing release. Stable
versions are refused while full acceptance remains incomplete.

## Owner setup

These are repository controls, not Terraform Registry requirements. The workflow
fails before signing if setup is missing; it does not provision its own access.

- Configure the `registry-release` environment with `nicholas-r-king` as a required
  reviewer. The owner has approved allowing self-review, so the owner may approve
  a release they triggered. Review is still required; do not bypass the gate.
  Keep other existing protection rules intact.
- Select deployment branches/tags and allow the release tag pattern as a **Tag**
  rule, for example `v*`. A `main` branch rule does not authorize version tags.
- With separate approval for transferring private signing material, store the
  approved ASCII-armored provider key as environment secret `GPG_PRIVATE_KEY` and
  its passphrase as environment secret `PASSPHRASE`. This puts a protected copy
  in GitHub; it is not local-only custody. Do not use repository-wide secrets or
  upload tenant credentials. Do not create or rotate a key merely to enable CI.
- Set environment variable `GPG_FINGERPRINT` to the approved full public primary
  fingerprint and `IRU_RELEASE_SETUP=reviewers-and-signing-configured`.
- After checking setup, set repository variable `IRU_RELEASE_ENABLED=true`.

The primary key must support RSA/DSA signing and be unexpired and enabled. Its
public key must already be registered for the correct Registry namespace. The
workflow forces that primary key, verifies the signature in a second public-only
keyring, and removes temporary keyrings and agents on exit. Private keys and
passphrases are never release assets or command-line arguments.

## Prepare a version

Review [API coverage](API_COVERAGE.md), [validation limits](VALIDATION_STATUS.md),
compatibility changes, dependency notices and the versioned changelog. Passing
synthetic or packaging checks does not establish complete live tenant acceptance.
Merge the reviewed source through normal checks and protections. Tag that exact
commit only after the release decision; never move an existing version tag.

The tag must be valid prerelease SemVer, such as `v0.2.0-alpha.1`, with a matching
`## 0.2.0-alpha.1` changelog section. It must identify a clean commit already
merged into `main`; no branch may share the tag's name. Do not create a tag through
another workflow's `GITHUB_TOKEN` and assume it triggers a new workflow: GitHub
suppresses most recursive Actions events.

## CI stages

1. Validate tag/source/changelog and the existing environment's review/tag rules.
2. Run minimum-Go and four native platform checks, including real Terraform mock
   lifecycles, vet, examples and deterministic docs. Build/test jobs have no signing
   secrets or release-write permission.
3. Build Darwin/Linux amd64/arm64 packages twice with pinned tools and compare
   checksums. Verify binaries, protocol-6 manifest, exact-source notices and legal
   material. Immutable artifacts identify the source, workflow run and build attempt.
4. Install every package natively through an offline mirror and export its schema.
5. After environment approval, recheck source/packages/policy, sign the checksum
   file and create a new experimental draft. Verify all seven uploaded asset
   names, sizes and SHA-256 digests, recheck the tag, then finalize the prerelease.
   Only this job has `contents: write`; it uses the built-in `GITHUB_TOKEN`.
6. Wait up to 15 minutes for Registry metadata on each native platform. Compare
   package checksums and the full signing fingerprint, then run clean direct
   `terraform init` with an explicit version and export schema. These checks use
   no developer overrides, remote backend, tenant credentials or live Iru calls.

Local checks before pushing workflow changes:

```sh
python3 -I -B -m unittest discover -s .github/scripts -p 'test_*.py'
go run ./tools verify -check-docs
```

The release contains four versioned ZIPs, a protocol manifest, SHA-256 checksums
and their binary detached GPG signature. GoReleaser publishing stays disabled;
only the final gated job writes the GitHub release. Local `tools/release`
identity/materials/verify/smoke commands remain unsigned preparation tools.

## Registry integration and failures

Follow [HashiCorp's publishing instructions](https://developer.hashicorp.com/terraform/registry/providers/publishing).
An individual GitHub account can publish; an HCP organization is optional. The
existing provider integration's `release` webhook should ingest future published
versions. Verify its status and the namespace's public signing key before release;
do not re-register keys or change webhooks without approval. No Registry token is
needed by this workflow. Prereleases are installable with an explicit constraint;
Terraform does not choose them automatically.

A failed signing/upload/publication stage may leave a draft or an already public
release if a response was lost. Inspect the exact tag, assets and signatures before
retrying. CI never deletes/clobbers an existing release or reuses a version with
different bytes. Reconcile partial drafts through a separate owner decision.
If only Registry verification failed, inspect ingestion and rerun those failed
verification jobs; do not rebuild or replace the published assets. Detached
installer uploads in Iru are unrelated to provider release artifacts.
