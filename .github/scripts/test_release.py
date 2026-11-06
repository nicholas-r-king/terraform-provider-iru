# SPDX-License-Identifier: MPL-2.0
import copy
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time
import unittest
from unittest import mock
import urllib.error

SPEC = importlib.util.spec_from_file_location("provider_release", Path(__file__).with_name("release.py"))
release = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(release)
SHA = "a" * 40
FPR = "8" * 40
TAG = "v0.2.0-alpha.1"


def key_record(fingerprint=FPR, validity="u", algorithm="1", expiry="200", capabilities="sc"):
    fields = ["sec", validity, "4096", algorithm, "", "", expiry, "", "", "", "", capabilities]
    return ":".join(fields) + "\nfpr:::::::::" + fingerprint + ":\n"


class ReleasePolicyTests(unittest.TestCase):
    def test_identity_rejects_stable_and_unsafe_refs(self):
        self.assertEqual(release.identity(TAG, SHA), "0.2.0-alpha.1")
        for tag in ("v0.2.0", "v0.2.0-alpha.01", "v0.2.0-alpha/other", "v0.2.0-alpha+build", "v00.2.0-alpha"):
            with self.subTest(tag=tag), self.assertRaises(release.ReleaseError):
                release.identity(tag, SHA)

    def test_environment_must_be_protected_and_allow_the_tag(self):
        environment = {"protection_rules": [{"type": "required_reviewers", "prevent_self_review": False, "reviewers": [{"type": "User", "reviewer": {"login": "nicholas-r-king"}}]}], "deployment_branch_policy": {"custom_branch_policies": True}}
        policies = {"branch_policies": [{"name": "v*", "type": "tag"}]}
        release.validate_policy(environment, policies, TAG)
        for change in ("no-rules", "wrong-reviewer", "additional-reviewer", "wrong-reviewer-type", "no-reviewers", "unrestricted", "branch-only", "wrong-tag"):
            env, rules = copy.deepcopy(environment), copy.deepcopy(policies)
            if change == "no-rules": env["protection_rules"] = []
            if change == "wrong-reviewer": env["protection_rules"][0]["reviewers"][0]["reviewer"]["login"] = "someone-else"
            if change == "additional-reviewer": env["protection_rules"][0]["reviewers"].append({"type": "User", "reviewer": {"login": "someone-else"}})
            if change == "wrong-reviewer-type": env["protection_rules"][0]["reviewers"][0]["type"] = "Team"
            if change == "no-reviewers": env["protection_rules"][0]["reviewers"] = []
            if change == "unrestricted": env["deployment_branch_policy"] = None
            if change == "branch-only": rules["branch_policies"][0]["type"] = "branch"
            if change == "wrong-tag": rules["branch_policies"][0]["name"] = "v1.*"
            with self.subTest(change=change), self.assertRaises(release.ReleaseError):
                release.validate_policy(env, rules, TAG)

    def test_owner_review_also_accepts_stricter_self_review_protection(self):
        environment = {"protection_rules": [{"type": "required_reviewers", "prevent_self_review": True,
                       "reviewers": [{"type": "User", "reviewer": {"login": "nicholas-r-king"}}]}],
                       "deployment_branch_policy": {"custom_branch_policies": True}}
        release.validate_policy(environment, {"branch_policies": [{"name": "v*", "type": "tag"}]}, TAG)

    def test_disabled_release_does_not_access_or_create_an_environment(self):
        with mock.patch.dict(os.environ, {"GITHUB_REPOSITORY": release.REPOSITORY}, clear=True), mock.patch.object(release, "gh_api") as api:
            with self.assertRaises(release.ReleaseError): release.policy(TAG, SHA)
            api.assert_not_called()

    def test_lightweight_and_annotated_tag_identity(self):
        direct = {"object": {"type": "commit", "sha": SHA}}
        self.assertEqual(release.check_tag(TAG, SHA, lambda _: direct), direct["object"])
        tag_sha = "b" * 40
        annotated = {"object": {"type": "tag", "sha": tag_sha}}
        self.assertEqual(release.check_tag(TAG, SHA, lambda path: annotated if path.startswith("git/ref/") else direct), annotated["object"])
        for data in ({"object": {"type": "commit", "sha": "c" * 40}}, {"object": {"type": "tree", "sha": SHA}}, {}):
            with self.assertRaises(release.ReleaseError): release.check_tag(TAG, SHA, lambda _: data)

    def test_signing_primary_identity_algorithm_expiry_and_capabilities(self):
        self.assertEqual(release.signing_key(key_record(), FPR, now=100), FPR)
        for data in (key_record("9" * 40), key_record(validity="r"), key_record(expiry="50"), key_record(algorithm="22"), key_record(capabilities="c"), key_record(capabilities="scD"), "ssb:u:4096:1::::::::s\nfpr:::::::::" + FPR + ":\n"):
            with self.assertRaises(release.ReleaseError): release.signing_key(data, FPR, now=100)

    def test_notes_include_only_the_version_section_and_honest_limits(self):
        text = release.notes(TAG, SHA, "## Unreleased\nfuture\n## 0.2.0-alpha.1\nSelected changes\n## 0.1.0-alpha.1\nold\n")
        self.assertIn("Selected changes", text)
        self.assertIn("full live acceptance remains incomplete", text)
        self.assertIn(SHA, text)
        self.assertNotIn("future", text)
        self.assertNotIn("\nold", text)
        with self.assertRaises(release.ReleaseError): release.notes(TAG, SHA, "## Unreleased\n")

    def test_uploaded_assets_must_match_every_local_byte_before_publication(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            prefix = "terraform-provider-iru_0.2.0-alpha.1"
            names = [prefix + "_" + target + ".zip" for target in release.TARGETS] + [prefix + "_manifest.json", prefix + "_SHA256SUMS", prefix + "_SHA256SUMS.sig"]
            for name in names: (root / name).write_bytes(b"synthetic artifact")
            (root / (prefix + "_SHA256SUMS")).write_text("".join(hashlib.sha256((root / name).read_bytes()).hexdigest() + "  " + name + "\n" for name in names if name.endswith((".zip", "_manifest.json"))))
            assets = [{"name": p.name, "state": "uploaded", "size": p.stat().st_size, "digest": "sha256:" + hashlib.sha256(p.read_bytes()).hexdigest()} for p in root.iterdir()]
            good = {"id": 1, "draft": True, "prerelease": True, "tag_name": TAG, "target_commitish": SHA, "body": "<!-- iru-release-source=" + SHA + " -->", "assets": assets}
            self.assertEqual(release.validate_assets(good, root, TAG, SHA), 1)
            for change in ("published", "stable", "wrong-source", "no-provenance", "missing", "duplicate", "wrong-hash", "incomplete"):
                data = copy.deepcopy(good)
                if change == "published": data["draft"] = False
                if change == "stable": data["prerelease"] = False
                if change == "wrong-source": data["tag_name"] = "v0.1.0-alpha.1"
                if change == "no-provenance": data["body"] = ""
                if change == "missing": data["assets"].pop()
                if change == "duplicate": data["assets"][-1] = data["assets"][0]
                if change == "wrong-hash": data["assets"][0]["digest"] = "sha256:" + "0" * 64
                if change == "incomplete": data["assets"][0]["state"] = "new"
                with self.subTest(change=change), self.assertRaises(release.ReleaseError): release.validate_assets(data, root, TAG, SHA)
            (root / names[0]).unlink()
            (root / names[0]).symlink_to(root / names[1])
            with self.assertRaises(release.ReleaseError): release.validate_assets(good, root, TAG, SHA)

    def test_public_key_inspection_has_no_credentials_and_accepts_only_primary(self):
        armor = "-----BEGIN PGP PUBLIC KEY BLOCK-----\nsynthetic\n"
        primary = ("pub::::::::::::\nfpr:::::::::" + FPR + ":\n").encode()
        with tempfile.TemporaryDirectory() as home, mock.patch.dict(os.environ, {"GH_TOKEN": "synthetic-secret"}), mock.patch.object(release.subprocess, "run", return_value=subprocess.CompletedProcess([], 0, primary)) as run:
            self.assertTrue(release.public_fingerprint(armor, FPR, home))
            self.assertNotIn("GH_TOKEN", run.call_args.kwargs["env"])
            self.assertEqual(run.call_args.kwargs["env"]["GNUPGHOME"], home)
            run.return_value.stdout = ("pub::::::::::::\nfpr:::::::::" + "9" * 40 + ":\nsub::::::::::::\nfpr:::::::::" + FPR + ":\n").encode()
            self.assertFalse(release.public_fingerprint(armor, FPR, home))
            with self.assertRaises(release.ReleaseError): release.public_fingerprint("PRIVATE KEY", FPR, home)


class RegistryTests(unittest.TestCase):
    def exercise(self, mode="ok", http=None):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        directory = Path(temporary.name)
        prefix = "terraform-provider-iru_0.2.0-alpha.1"
        names = [prefix + "_" + x + ".zip" for x in release.TARGETS] + [prefix + "_manifest.json"]
        (directory / (prefix + "_SHA256SUMS")).write_text("".join("1" * 64 + "  " + name + "\n" for name in names))
        metadata = {"filename": prefix + "_linux_amd64.zip", "os": "linux", "arch": "amd64", "shasum": "1" * 64, "protocols": ["6.0"], "signing_keys": {"gpg_public_keys": [{"ascii_armor": "synthetic public key"}]}}
        if mode == "wrong-checksum": metadata["shasum"] = "2" * 64
        calls, workspaces = [], []

        def command(args, **kwargs):
            calls.append(args)
            work = Path(kwargs["cwd"])
            workspaces.append(work)
            self.assertNotIn("GH_TOKEN", kwargs["env"])
            self.assertNotIn("TF_CLI_ARGS", kwargs["env"])
            self.assertEqual(kwargs["env"]["IRU_API_TOKEN"], "synthetic-release-verification")
            self.assertNotIn("filesystem_mirror", Path(kwargs["env"]["TF_CLI_CONFIG_FILE"]).read_text())
            self.assertEqual(Path(kwargs["env"]["TF_CLI_CONFIG_FILE"]).stat().st_mode & 0o777, 0o600)
            if args[1] == "init":
                (work / ".terraform.lock.hcl").write_text('version     = "0.2.0-alpha.1"\nzh:' + "1" * 64)
                return subprocess.CompletedProcess(args, 1 if mode == "init-failure" else 0, ("signed key ID " + FPR[-16:]).encode())
            self.assertEqual(args, ["terraform", "providers", "schema", "-json"])
            data = {"provider_schemas": {release.ADDRESS: {"resource_schemas": {"synthetic": {}}, "data_source_schemas": {"synthetic": {}}}}}
            return subprocess.CompletedProcess(args, 0, json.dumps(data).encode())

        responses = [urllib.error.HTTPError("synthetic", http, "synthetic", {}, None), io.BytesIO(json.dumps(metadata).encode())] if http else [io.BytesIO(json.dumps(metadata).encode())]
        with mock.patch.object(release.platform, "system", return_value="Linux"), mock.patch.object(release.platform, "machine", return_value="x86_64"), mock.patch.object(release.urllib.request, "urlopen", side_effect=responses), mock.patch.object(release, "public_fingerprint", return_value=mode != "wrong-key"), mock.patch.object(release.subprocess, "run", side_effect=command), mock.patch.object(release.time, "sleep") as sleep, mock.patch.dict(os.environ, {"GH_TOKEN": "synthetic-other-secret", "IRU_API_TOKEN": "synthetic-tenant-secret", "TF_CLI_ARGS": "synthetic-override"}), mock.patch("sys.stdout", new_callable=io.StringIO):
            if mode == "ok" and http != 403:
                release.registry(TAG, SHA, directory, FPR)
                self.assertEqual(len(calls), 2)
            else:
                with self.assertRaises(release.ReleaseError): release.registry(TAG, SHA, directory, FPR)
                if mode in ("wrong-checksum", "wrong-key") or http == 403: self.assertEqual(calls, [])
            if http in (404, 429, 503): sleep.assert_called_once()
            elif http == 403: sleep.assert_not_called()
        self.assertTrue(all(not p.exists() for p in workspaces), "sensitive temporary workspaces must be removed")

    def test_direct_native_install_and_schema_with_clean_environment(self): self.exercise()
    def test_pending_ingestion_retries_only_reads(self): self.exercise(http=404)
    def test_transient_metadata_failure_retries_only_reads(self): self.exercise(http=503)
    def test_authorization_is_not_mistaken_for_absence(self): self.exercise(http=403)
    def test_checksum_mismatch_prevents_execution(self): self.exercise("wrong-checksum")
    def test_key_mismatch_prevents_execution(self): self.exercise("wrong-key")
    def test_install_failure_cleans_workspace(self): self.exercise("init-failure")

    def test_ingestion_timeout_stops_without_install_or_publication(self):
        with tempfile.TemporaryDirectory() as temporary:
            prefix = "terraform-provider-iru_0.2.0-alpha.1"
            names = [prefix + "_" + target + ".zip" for target in release.TARGETS] + [prefix + "_manifest.json"]
            Path(temporary, prefix + "_SHA256SUMS").write_text("".join("1" * 64 + "  " + name + "\n" for name in names))
            failure = urllib.error.HTTPError("synthetic", 404, "synthetic", {}, None)
            with mock.patch.object(release.platform, "system", return_value="Linux"), mock.patch.object(release.platform, "machine", return_value="x86_64"), mock.patch.object(release.urllib.request, "urlopen", side_effect=failure), mock.patch.object(release.time, "monotonic", side_effect=[0, 901]), mock.patch.object(release.subprocess, "run") as run, mock.patch.object(release.time, "sleep") as sleep:
                with self.assertRaisesRegex(release.ReleaseError, "15 minutes"):
                    release.registry(TAG, SHA, temporary, FPR)
                run.assert_not_called()
                sleep.assert_not_called()


class PublishScriptTests(unittest.TestCase):
    def test_shell_syntax(self):
        subprocess.run(["bash", "-n", str(Path(__file__).with_name("publish-release.sh"))], check=True)

    def exercise(self, mode):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            scripts = root / ".github/scripts"
            scripts.mkdir(parents=True)
            for name in ("release.py", "publish-release.sh"):
                (scripts / name).write_bytes(Path(__file__).with_name(name).read_bytes())
            (root / "CHANGELOG.md").write_text("## 0.2.0-alpha.1\nSynthetic changes\n")
            dist, binaries, runner_temp = root / "dist", root / "bin", root / "runner"
            for directory in (dist, binaries, runner_temp): directory.mkdir()
            prefix = "terraform-provider-iru_0.2.0-alpha.1"
            names = [prefix + "_" + target + ".zip" for target in release.TARGETS] + [prefix + "_manifest.json"]
            for name in names: (dist / name).write_bytes(b"synthetic package")
            (dist / (prefix + "_SHA256SUMS")).write_text("".join(hashlib.sha256((dist / name).read_bytes()).hexdigest() + "  " + name + "\n" for name in names))
            if mode == "tampered": (dist / names[0]).write_bytes(b"changed")
            if mode == "extra-file": (dist / "unapproved.zip").write_bytes(b"synthetic extra data")
            (root / "settings").write_text(json.dumps({"mode": mode, "fingerprint": FPR, "tag": TAG, "commit": SHA}))
            fake = '''
import hashlib,json,os,pathlib,sys,time
name=pathlib.Path(sys.argv[0]).name
args=sys.argv[1:]
root=pathlib.Path(sys.argv[0]).resolve().parent.parent
settings=json.loads((root/'settings').read_text())
mode=settings['mode']
with (root/'calls').open('a') as out: out.write(json.dumps([name,args])+'\\n')
if name in ('gpg','gpgconf') and any(os.environ.get(k) for k in ('GH_TOKEN','GITHUB_TOKEN','private_key','passphrase')): sys.exit(97)
if name=='gh' and any(os.environ.get(k) for k in ('GPG_PRIVATE_KEY','PASSPHRASE','private_key','passphrase')): sys.exit(98)
if name=='gpgconf': sys.exit(0)
if name=='sha256sum':
    for line in pathlib.Path(args[-1]).read_text().splitlines():
        sha,file=line.split('  ')
        if hashlib.sha256(pathlib.Path(file).read_bytes()).hexdigest()!=sha: sys.exit(1)
    sys.exit(0)
fpr=settings['fingerprint']
if name=='gpg':
    if '--import' in args: sys.stdin.buffer.read(); sys.exit(0)
    if '--list-secret-keys' in args:
        if mode=='wrong-key': fpr='9'*40
        print('sec:u:4096:1:::4102444800:::::sc')
        print('fpr:::::::::'+fpr+':'); sys.exit(0)
    if '--detach-sign' in args:
        assert '--passphrase-fd' in args and '--passphrase' not in args
        assert sys.stdin.read()=='synthetic-passphrase'
        assert args[args.index('--local-user')+1]==fpr+'!'
        if mode=='interrupted':
            (root/'signing-started').touch();time.sleep(30)
        pathlib.Path(args[args.index('--output')+1]).write_bytes(b'\\x89synthetic-signature'); sys.exit(0)
    if '--export' in args: print('synthetic-public-key'); sys.exit(0)
    if '--verify' in args:
        if mode=='bad-signature': sys.exit(1)
        print('[GNUPG:] VALIDSIG '+fpr+' synthetic'); sys.exit(0)
    sys.exit(70)
if name=='gh':
    if args[:2]==['release','create']:
        if mode=='existing-release': sys.exit(1)
        assert '--verify-tag' in args and '--draft' in args and '--clobber' not in args
        body=pathlib.Path(args[args.index('--notes-file')+1]).read_text()
        assets=[dict(name=p.name,state='uploaded',size=p.stat().st_size,digest='sha256:'+hashlib.sha256(p.read_bytes()).hexdigest()) for p in (root/'dist').iterdir()]
        if mode=='wrong-digest': assets[0]['digest']='sha256:'+'0'*64
        data=dict(id=123,draft=True,prerelease=True,tag_name=settings['tag'],target_commitish=settings['commit'],body=body,assets=assets,html_url='https://github.com/nicholas-r-king/terraform-provider-iru/releases/tag/'+settings['tag'])
        (root/'metadata').write_text(json.dumps(data)); sys.exit(0)
    assert args[0]=='api'
    path=next(x for x in args if x.startswith('repos/'))
    if '/deployment-branch-policies' in path:
        print(json.dumps(dict(total_count=1,branch_policies=[dict(name='v*',type='tag')]))); sys.exit(0)
    if path.endswith('/environments/registry-release'):
        rules=[] if mode=='unprotected' else [dict(type='required_reviewers',prevent_self_review=False,reviewers=[dict(type='User',reviewer=dict(login='nicholas-r-king'))])]
        print(json.dumps(dict(protection_rules=rules,deployment_branch_policy=dict(custom_branch_policies=True)))); sys.exit(0)
    if '/git/ref/tags/' in path:
        counter=root/'tag-count'; count=int(counter.read_text())+1 if counter.exists() else 1; counter.write_text(str(count))
        sha='b'*40 if mode=='moved-tag' and count>1 else settings['commit']
        print(json.dumps(dict(object=dict(type='commit',sha=sha)))); sys.exit(0)
    data=json.loads((root/'metadata').read_text())
    if '--method' in args:
        assert args[args.index('--method')+1]=='PATCH'
        data.update(json.loads(pathlib.Path(args[args.index('--input')+1]).read_text()))
        (root/'metadata').write_text(json.dumps(data))
    print(json.dumps(data)); sys.exit(0)
sys.exit(71)
'''
            for name in ("gh", "gpg", "gpgconf", "sha256sum"):
                path = binaries / name
                path.write_text("#!" + sys.executable + "\n" + fake)
                path.chmod(0o700)
            env = {"PATH": str(binaries) + os.pathsep + str(Path(sys.executable).parent) + os.pathsep + os.defpath, "HOME": temporary, "RUNNER_TEMP": str(runner_temp), "MOCK_ROOT": temporary, "MOCK_MODE": mode, "GITHUB_REPOSITORY": release.REPOSITORY, "RELEASE_TAG": TAG, "RELEASE_COMMIT": SHA, "GPG_FINGERPRINT": FPR, "GPG_PRIVATE_KEY": "synthetic-private-key", "PASSPHRASE": "synthetic-passphrase", "GH_TOKEN": "synthetic-github-token", "IRU_RELEASE_ENABLED": "true", "IRU_RELEASE_SETUP": "reviewers-and-signing-configured", "GITHUB_OUTPUT": str(root / "output")}
            env.update(private_key="inherited-synthetic-secret", passphrase="inherited-synthetic-secret", SHELLOPTS="braceexpand:hashall:interactive-comments:xtrace")
            if mode == "missing-key": env.pop("GPG_PRIVATE_KEY")
            if mode == "interrupted":
                process = subprocess.Popen(["bash", str(scripts / "publish-release.sh")], cwd=root, env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE, start_new_session=True)
                try:
                    deadline = time.monotonic() + 10
                    while not (root / "signing-started").exists() and process.poll() is None and time.monotonic() < deadline:
                        time.sleep(0.05)
                    self.assertTrue((root / "signing-started").exists(), "signing stage did not start")
                    os.killpg(process.pid, signal.SIGTERM)
                    stdout, stderr = process.communicate(timeout=10)
                    result = subprocess.CompletedProcess(process.args, process.returncode, stdout, stderr)
                finally:
                    if process.poll() is None:
                        os.killpg(process.pid, signal.SIGKILL)
                        process.communicate(timeout=5)
            else:
                result = subprocess.run(["bash", str(scripts / "publish-release.sh")], cwd=root, env=env, capture_output=True, timeout=30)
            calls = [json.loads(line) for line in (root / "calls").read_text().splitlines()] if (root / "calls").exists() else []
            writes = [args for name, args in calls if name == "gh" and (args[:2] == ["release", "create"] or "PATCH" in args)]
            self.assertEqual(result.returncode == 0, mode == "ok", result.stderr.decode())
            self.assertFalse(list(runner_temp.iterdir()), "temporary signing homes must be removed on every outcome")
            for secret in ("synthetic-private-key", "synthetic-passphrase", "synthetic-github-token"):
                self.assertNotIn(secret, json.dumps(calls))
                self.assertNotIn(secret.encode(), result.stdout + result.stderr)
            if mode == "ok":
                self.assertEqual(len(writes), 2)
                self.assertFalse(json.loads((root / "metadata").read_text())["draft"])
                self.assertIn("fingerprint=" + FPR, (root / "output").read_text())
            elif mode in ("wrong-digest", "existing-release"):
                self.assertEqual(len(writes), 1)
                self.assertNotIn("PATCH", writes[0])
            else:
                self.assertEqual(writes, [])

    def test_sign_verify_upload_validate_then_publish(self): self.exercise("ok")
    def test_changed_bytes_prevent_signing_and_release_creation(self): self.exercise("tampered")
    def test_extra_files_prevent_upload(self): self.exercise("extra-file")
    def test_wrong_key_prevents_release_creation(self): self.exercise("wrong-key")
    def test_invalid_signature_prevents_release_creation(self): self.exercise("bad-signature")
    def test_changed_tag_prevents_release_creation(self): self.exercise("moved-tag")
    def test_lost_environment_protection_prevents_release_creation(self): self.exercise("unprotected")
    def test_bad_uploaded_digest_prevents_publication(self): self.exercise("wrong-digest")
    def test_existing_release_is_never_overwritten(self): self.exercise("existing-release")
    def test_missing_key_fails_before_any_github_request(self): self.exercise("missing-key")
    def test_interruption_removes_signing_homes(self): self.exercise("interrupted")


if __name__ == "__main__":
    unittest.main()
