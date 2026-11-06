#!/usr/bin/env python3
# SPDX-License-Identifier: MPL-2.0
"""Release policy, immutable-asset checks, and public Registry verification."""

import argparse
import fnmatch
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

REPOSITORY = "nicholas-r-king/terraform-provider-iru"
ADDRESS = "registry.terraform.io/nicholas-r-king/iru"
TAG = re.compile(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)")
FINGERPRINT = re.compile(r"(?:[A-Fa-f0-9]{40}|[A-Fa-f0-9]{64})")
TARGETS = ("darwin_amd64", "darwin_arm64", "linux_amd64", "linux_arm64")


class ReleaseError(Exception):
    pass


def require(condition, message):
    if not condition:
        raise ReleaseError(message)


def identity(tag, commit):
    match = TAG.fullmatch(tag)
    require(match and re.fullmatch(r"[a-f0-9]{40}", commit), "a prerelease SemVer tag and exact source SHA are required")
    require(all(not (x.isdigit() and len(x) > 1 and x[0] == "0") for x in match[4].split(".")), "invalid numeric prerelease identifier")
    return tag[1:]


def gh_api(path):
    # Only GitHub CLI receives its token. Never inherit signing or tenant secrets.
    env = {k: v for k, v in os.environ.items() if k in ("PATH", "HOME", "TMPDIR", "SYSTEMROOT", "GH_TOKEN", "GITHUB_TOKEN")}
    env["GH_PROMPT_DISABLED"] = "1"
    result = subprocess.run(["gh", "api", "repos/" + REPOSITORY + "/" + path], env=env, capture_output=True, timeout=60)
    require(result.returncode == 0, "GitHub metadata request failed: " + path)
    try:
        data = json.loads(result.stdout)
    except ValueError:
        raise ReleaseError("GitHub metadata was not valid JSON") from None
    require(isinstance(data, dict), "GitHub metadata was not an object")
    return data


def check_tag(tag, commit, api=gh_api):
    identity(tag, commit)
    obj = api("git/ref/tags/" + tag).get("object", {})
    original = dict(obj)
    for _ in range(5):
        if obj.get("type") == "commit":
            require(obj.get("sha") == commit, "release tag no longer identifies the tested commit")
            return original
        require(obj.get("type") == "tag" and re.fullmatch(r"[a-f0-9]{40}", obj.get("sha", "")), "invalid release tag object")
        obj = api("git/tags/" + obj["sha"]).get("object", {})
    raise ReleaseError("release tag nesting is unsupported")


def validate_policy(environment, policies, tag):
    reviewers = [r for r in environment.get("protection_rules", []) if r.get("type") == "required_reviewers"]
    require(any(len(r.get("reviewers", [])) == 1 and
                r["reviewers"][0].get("type") == "User" and
                r["reviewers"][0].get("reviewer", {}).get("login") == "nicholas-r-king"
                for r in reviewers),
            "registry-release must require review by nicholas-r-king")
    policy = environment.get("deployment_branch_policy") or {}
    require(policy.get("custom_branch_policies") is True, "registry-release must restrict selected branches and tags")
    require(any(p.get("type") == "tag" and fnmatch.fnmatchcase(tag, p.get("name", "")) for p in policies.get("branch_policies", [])), "registry-release does not allow this version tag")


def policy(tag, commit):
    identity(tag, commit)
    require(os.environ.get("GITHUB_REPOSITORY") == REPOSITORY, "release repository does not match")
    require(os.environ.get("IRU_RELEASE_ENABLED") == "true", "IRU_RELEASE_ENABLED is not enabled by the owner")
    check_tag(tag, commit)
    environment = gh_api("environments/registry-release")
    policies = gh_api("environments/registry-release/deployment-branch-policies?per_page=100")
    require(policies.get("total_count", 0) <= 100, "deployment policy listing is incomplete")
    validate_policy(environment, policies, tag)


def signing_key(raw, fingerprint, now=None):
    require(FINGERPRINT.fullmatch(fingerprint), "invalid approved signing fingerprint")
    primary = None
    for line in raw.splitlines():
        fields = line.split(":")
        if fields[0] == "sec":
            require(len(fields) >= 12, "incomplete signing-key metadata")
            primary = fields
        elif fields[0] == "ssb":
            primary = None
        elif fields[0] == "fpr" and primary:
            if len(fields) > 9 and fields[9].upper() == fingerprint.upper():
                require(primary[1] not in ("r", "e", "d") and "D" not in primary[11], "signing key is revoked, expired or disabled")
                require(primary[3] in ("1", "3", "17") and "s" in primary[11], "approved primary key must support RSA/DSA signing")
                require(primary[6].isdigit(), "invalid signing-key expiry")
                require(int(primary[6]) == 0 or int(primary[6]) > (time.time() if now is None else now), "signing key expired")
                return fingerprint.upper()
            primary = None
    raise ReleaseError("approved primary secret key was not found")


def notes(tag, commit, changelog):
    version = identity(tag, commit)
    section = re.search(r"(?ms)^## " + re.escape(version) + r"\n(.*?)(?=^## |\Z)", changelog)
    require(section and section[1].strip(), "versioned changelog is missing")
    return ("Experimental, unofficial Iru provider prerelease. Selected live tests and automated checks have passed; full live acceptance remains incomplete.\n\n"
            + section[1].strip() + "\n\nValidation and limits: https://github.com/" + REPOSITORY + "/blob/" + commit + "/VALIDATION_STATUS.md\n"
            + "\n<!-- iru-release-source=" + commit + " -->\n")


def file_sha256(path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1 << 20), b""):
            digest.update(chunk)
    return digest.hexdigest()


def local_assets(directory, tag, commit, signed):
    version = identity(tag, commit)
    prefix = "terraform-provider-iru_" + version
    names = {prefix + "_" + target + ".zip" for target in TARGETS}
    names.add(prefix + "_manifest.json")
    checksummed = set(names)
    names.add(prefix + "_SHA256SUMS")
    if signed:
        names.add(prefix + "_SHA256SUMS.sig")
    directory = Path(directory)
    require({p.name for p in directory.iterdir()} == names, "release asset directory has missing or unexpected files")
    require(all((directory / name).is_file() and not (directory / name).is_symlink() for name in names), "release assets must be regular files")
    lines = (directory / (prefix + "_SHA256SUMS")).read_text().splitlines()
    pairs = [re.fullmatch(r"([a-f0-9]{64})  ([A-Za-z0-9_.-]+)", line) for line in lines]
    require(len(lines) == 5 and all(pairs) and {p[2] for p in pairs} == checksummed, "invalid release checksum inventory")
    require(all(file_sha256(directory / pair[2]) == pair[1] for pair in pairs), "release checksum does not match local bytes")
    if signed:
        signature = directory / (prefix + "_SHA256SUMS.sig")
        require(signature.stat().st_size > 0 and not signature.read_bytes().startswith(b"-----BEGIN"), "checksum signature must be binary and nonempty")
    return names


def validate_assets(metadata, directory, tag, commit):
    names = local_assets(directory, tag, commit, True)
    directory = Path(directory)
    require(isinstance(metadata, dict), "invalid release metadata")
    require(metadata.get("draft") is True and metadata.get("prerelease") is True, "only a new experimental draft can be finalized")
    require(metadata.get("tag_name") == tag, "draft release tag does not match")
    require("<!-- iru-release-source=" + commit + " -->" in metadata.get("body", ""), "draft provenance does not match")
    assets = metadata.get("assets", [])
    require(isinstance(assets, list) and all(isinstance(a, dict) for a in assets), "invalid uploaded asset metadata")
    require(len(assets) == len(names) and {a.get("name") for a in assets} == names, "uploaded asset inventory does not match")
    for asset in assets:
        path = directory / asset["name"]
        require(path.is_file() and not path.is_symlink(), "release asset must be a regular file")
        require(asset.get("state") == "uploaded" and asset.get("size") == path.stat().st_size, "asset upload is incomplete")
        require(asset.get("digest") == "sha256:" + file_sha256(path), "uploaded asset digest does not match")
    require(type(metadata.get("id")) is int and metadata["id"] > 0, "invalid release ID")
    return metadata["id"]


def public_fingerprint(armor, expected, home):
    require(armor.startswith("-----BEGIN PGP PUBLIC KEY BLOCK-----") and "PRIVATE KEY" not in armor, "Registry signing key is not a public key")
    env = {"PATH": os.environ.get("PATH", os.defpath), "HOME": str(home), "GNUPGHOME": str(home)}
    result = subprocess.run(["gpg", "--batch", "--with-colons", "--show-keys"], input=armor.encode(), env=env, capture_output=True, timeout=30)
    require(result.returncode == 0, "Registry signing key could not be inspected")
    primary = False
    for line in result.stdout.decode().splitlines():
        fields = line.split(":")
        if fields[0] in ("pub", "sub"):
            primary = fields[0] == "pub"
        elif fields[0] == "fpr" and primary:
            if len(fields) > 9 and fields[9].upper() == expected.upper():
                return True
            primary = False
    return False


def registry(tag, commit, directory, fingerprint):
    version = identity(tag, commit)
    require(FINGERPRINT.fullmatch(fingerprint), "missing verified publication fingerprint")
    os_name = {"Linux": "linux", "Darwin": "darwin"}.get(platform.system())
    arch = {"x86_64": "amd64", "arm64": "arm64", "aarch64": "arm64"}.get(platform.machine().lower())
    require(os_name and arch, "Registry verification requires a supported native runner")
    filename = "terraform-provider-iru_" + version + "_" + os_name + "_" + arch + ".zip"
    sums = Path(directory, "terraform-provider-iru_" + version + "_SHA256SUMS").read_text()
    entries = re.findall(r"(?m)^([a-f0-9]{64})  ([A-Za-z0-9_.-]+)$", sums)
    hashes = {name: sha for sha, name in entries}
    expected_names = {"terraform-provider-iru_" + version + "_" + target + ".zip" for target in TARGETS}
    expected_names.add("terraform-provider-iru_" + version + "_manifest.json")
    require(len(sums.splitlines()) == 5 and len(entries) == 5 and set(hashes) == expected_names, "invalid expected checksum inventory")
    url = "https://registry.terraform.io/v1/providers/nicholas-r-king/iru/" + version + "/download/" + os_name + "/" + arch
    deadline = time.monotonic() + 15 * 60
    while True:
        try:
            with urllib.request.urlopen(url, timeout=15) as response:
                raw = response.read((1 << 20) + 1)
                require(len(raw) <= 1 << 20, "Registry metadata exceeds its size bound")
                data = json.loads(raw)
            break
        except urllib.error.HTTPError as error:
            status = error.code
            error.close()
            require(status == 404 or status == 429 or status >= 500, "Registry metadata request failed with HTTP " + str(status))
        except urllib.error.URLError:
            pass
        require(time.monotonic() < deadline, "Registry ingestion was not verified within 15 minutes; the GitHub release may already be public")
        time.sleep(min(20, max(0, deadline - time.monotonic())))
    require(isinstance(data, dict), "Registry metadata was not an object")
    require(data.get("filename") == filename and data.get("shasum") == hashes[filename] and data.get("os") == os_name and data.get("arch") == arch, "Registry package identity/checksum does not match the tested artifact")
    require("6.0" in data.get("protocols", []), "Registry protocol metadata does not match")
    with tempfile.TemporaryDirectory(prefix="iru-registry-") as temporary:
        work = Path(temporary)
        key_home = work / "gnupg"
        key_home.mkdir(mode=0o700)
        require(any(public_fingerprint(k.get("ascii_armor", ""), fingerprint, key_home) for k in data.get("signing_keys", {}).get("gpg_public_keys", [])), "Registry signing fingerprint does not match")
        config = work / "terraform.tfrc"
        config.write_text('provider_installation { direct { include = ["' + ADDRESS + '"] } }\n')
        config.chmod(0o600)
        (work / "main.tf").write_text('terraform {\n required_providers {\n iru = { source = "nicholas-r-king/iru"\n version = "= ' + version + '" }\n }\n}\n')
        env = {"PATH": os.environ.get("PATH", os.defpath), "HOME": temporary, "TMPDIR": temporary, "TF_CLI_CONFIG_FILE": str(config), "TF_IN_AUTOMATION": "1", "TF_INPUT": "0", "CHECKPOINT_DISABLE": "1", "IRU_API_URL": "https://synthetic.api.iru.com", "IRU_API_TOKEN": "synthetic-release-verification"}
        result = subprocess.run(["terraform", "init", "-backend=false", "-input=false", "-no-color"], cwd=work, env=env, capture_output=True, timeout=300)
        require(result.returncode == 0, "Registry terraform init failed")
        require(fingerprint[-16:].lower() in result.stdout.decode().lower(), "Terraform did not report the expected signing identity")
        lock = (work / ".terraform.lock.hcl").read_text()
        require(re.search(r'(?m)^\s*version\s*=\s*"' + re.escape(version) + r'"\s*$', lock) and "zh:" + hashes[filename] in lock, "installed provider lock does not match")
        result = subprocess.run(["terraform", "providers", "schema", "-json"], cwd=work, env=env, capture_output=True, timeout=120)
        require(result.returncode == 0, "Registry-installed schema export failed")
        schema = json.loads(result.stdout).get("provider_schemas", {}).get(ADDRESS, {})
        resources, lookups = schema.get("resource_schemas", {}), schema.get("data_source_schemas", {})
        require(resources and lookups, "Registry-installed schema is incomplete")
        print(json.dumps({"version": version, "platform": os_name + "/" + arch, "fingerprint": fingerprint.upper(), "resources": len(resources), "data_sources": len(lookups), "registry_install": "passed"}))


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("action", choices=("policy", "tag", "notes", "assets", "local-assets", "signing-key", "registry"))
    parser.add_argument("--tag", default=os.environ.get("RELEASE_TAG", ""))
    parser.add_argument("--commit", default=os.environ.get("RELEASE_COMMIT", ""))
    parser.add_argument("--dir", default="dist")
    parser.add_argument("--metadata")
    parser.add_argument("--signed", action="store_true")
    parser.add_argument("--fingerprint", default=os.environ.get("GPG_FINGERPRINT", ""))
    args = parser.parse_args()
    if args.action == "policy":
        policy(args.tag, args.commit)
    elif args.action == "tag":
        print(json.dumps(check_tag(args.tag, args.commit), sort_keys=True))
    elif args.action == "notes":
        print(notes(args.tag, args.commit, Path("CHANGELOG.md").read_text()), end="")
    elif args.action == "assets":
        print(validate_assets(json.loads(Path(args.metadata).read_text()), args.dir, args.tag, args.commit))
    elif args.action == "local-assets":
        print("\n".join(sorted(local_assets(args.dir, args.tag, args.commit, args.signed))))
    elif args.action == "signing-key":
        import sys
        print(signing_key(sys.stdin.read(), args.fingerprint))
    else:
        registry(args.tag, args.commit, args.dir, args.fingerprint)


if __name__ == "__main__":
    try:
        main()
    except (ReleaseError, subprocess.SubprocessError, OSError, ValueError) as error:
        # Subprocess output and HTTP response bodies can contain sensitive data.
        message = str(error) if isinstance(error, ReleaseError) else "release operation failed; inspect the named stage without exposing secret values"
        raise SystemExit(message) from None
