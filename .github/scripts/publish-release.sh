#!/usr/bin/env bash
# SPDX-License-Identifier: MPL-2.0
set +x
set -euo pipefail
umask 077

fail() { printf '%s\n' "$1" >&2; exit 1; }
test "${GITHUB_REPOSITORY:-}" = 'nicholas-r-king/terraform-provider-iru' || fail 'Unexpected release repository'
test "${IRU_RELEASE_ENABLED:-}" = 'true' || fail 'Owner release enablement is missing'
test "${IRU_RELEASE_SETUP:-}" = 'reviewers-and-signing-configured' || fail 'Owner signing setup is not confirmed'
: "${GH_TOKEN:?GitHub release token is unavailable}"
: "${GPG_PRIVATE_KEY:?registry-release GPG_PRIVATE_KEY is not configured}"
: "${PASSPHRASE:?registry-release PASSPHRASE is not configured}"
[[ "${GPG_FINGERPRINT:-}" =~ ^([A-Fa-f0-9]{40}|[A-Fa-f0-9]{64})$ ]] || fail 'Approved GPG_FINGERPRINT is missing or invalid'

private_key=$GPG_PRIVATE_KEY
passphrase=$PASSPHRASE
export -n private_key passphrase
unset GPG_PRIVATE_KEY PASSPHRASE
work=$(mktemp -d "${RUNNER_TEMP:?}/iru-sign.XXXXXXXX")
sign_home="$work/sign"
verify_home="$work/verify"
mkdir -m 700 "$sign_home" "$verify_home"
safe_gpg() { env -u GH_TOKEN -u GITHUB_TOKEN -u GPG_PRIVATE_KEY -u PASSPHRASE gpg --batch --no-tty "$@"; }
cleanup() {
  env -u GH_TOKEN -u GITHUB_TOKEN GNUPGHOME="$sign_home" gpgconf --kill gpg-agent >/dev/null 2>&1 || true
  env -u GH_TOKEN -u GITHUB_TOKEN GNUPGHOME="$verify_home" gpgconf --kill gpg-agent >/dev/null 2>&1 || true
  rm -rf -- "$work"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP
stage=policy
trap 'printf "Release failed during %s; inspect any existing draft before retrying.\n" "$stage" >&2' ERR

python3 .github/scripts/release.py policy
stage=tag
python3 .github/scripts/release.py tag > "$work/tag-before.json"
sums="terraform-provider-iru_${RELEASE_TAG#v}_SHA256SUMS"
stage=checksums
python3 .github/scripts/release.py local-assets > "$work/unsigned-assets"
stage=key-import
printf '%s' "$private_key" | safe_gpg --homedir "$sign_home" --import >"$work/import.log" 2>&1
unset private_key
safe_gpg --homedir "$sign_home" --with-colons --list-secret-keys --fingerprint "$GPG_FINGERPRINT" > "$work/key.txt" 2>"$work/key.log"
stage=key-identity
env -u GH_TOKEN -u GITHUB_TOKEN python3 .github/scripts/release.py signing-key < "$work/key.txt" > "$work/fingerprint"
fingerprint=$(< "$work/fingerprint")
stage=signing
printf '%s' "$passphrase" | safe_gpg --homedir "$sign_home" --pinentry-mode loopback --passphrase-fd 0 --local-user "${GPG_FINGERPRINT}!" --digest-algo SHA256 --no-armor --detach-sign --output "dist/$sums.sig" "dist/$sums" >"$work/sign.log" 2>&1
unset passphrase
safe_gpg --homedir "$sign_home" --armor --export "$GPG_FINGERPRINT" | safe_gpg --homedir "$verify_home" --import >"$work/public-import.log" 2>&1
stage=signature-verification
safe_gpg --homedir "$verify_home" --status-fd 1 --verify "dist/$sums.sig" "dist/$sums" > "$work/verification.txt" 2>"$work/verification.log"
grep -Eq "^\[GNUPG:\] VALIDSIG $fingerprint " "$work/verification.txt"
python3 .github/scripts/release.py notes > "$work/notes.md"
python3 .github/scripts/release.py tag > "$work/tag-current.json"
cmp "$work/tag-before.json" "$work/tag-current.json"
python3 .github/scripts/release.py local-assets --signed > "$work/assets"
assets=()
while IFS= read -r name; do assets+=("dist/$name"); done < "$work/assets"

# Creation never updates an existing release or clobbers an existing asset.
# Any failure may leave a draft; reconcile it before another attempt.
stage=draft-creation
gh release create "$RELEASE_TAG" "${assets[@]}" --repo "$GITHUB_REPOSITORY" --target "$RELEASE_COMMIT" --verify-tag --draft --prerelease --latest=false --title "$RELEASE_TAG (experimental)" --notes-file "$work/notes.md"
stage=uploaded-assets
gh api "repos/$GITHUB_REPOSITORY/releases/tags/$RELEASE_TAG" > "$work/release.json"
release_id=$(python3 .github/scripts/release.py assets --metadata "$work/release.json")
python3 .github/scripts/release.py tag > "$work/tag-final.json"
cmp "$work/tag-before.json" "$work/tag-final.json"
printf '{"draft":false,"prerelease":true,"make_latest":"false"}\n' > "$work/publish.json"
stage=publication
gh api --method PATCH "repos/$GITHUB_REPOSITORY/releases/$release_id" --input "$work/publish.json" > "$work/published.json"
python3 - "$work/published.json" "$RELEASE_TAG" <<'PY'
import json,sys
data=json.load(open(sys.argv[1]))
if data.get('draft') is not False or data.get('prerelease') is not True or data.get('tag_name')!=sys.argv[2]:
    raise SystemExit('release publication was not confirmed; inspect before retrying')
print('Published '+data['html_url'])
PY
printf 'fingerprint=%s\n' "$fingerprint" >> "$GITHUB_OUTPUT"
