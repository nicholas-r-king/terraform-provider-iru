// SPDX-License-Identifier: MPL-2.0

// Release preparation has no signing, network-publishing, or live-tenant code.
package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

const project = "terraform-provider-iru"
const module = "github.com/nicholas-r-king/" + project
const address = "registry.terraform.io/nicholas-r-king/iru"

var targets = []string{"darwin_amd64", "darwin_arm64", "linux_amd64", "linux_arm64"}
var tagPattern = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)$`)
var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

const legalModuleBoundary = "// Generated legal material; keep outside the provider's Go package traversal.\nmodule example.invalid/iru-release-legal-material\n\ngo 1.25.0\n"

func main() {
	if err := execute(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func execute(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: go run ./tools/release <identity|materials|verify|smoke> -tag vX.Y.Z-pre.N -commit SHA [-dir dist] [-terraform path]")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	tag := flags.String("tag", "", "existing prerelease SemVer tag")
	commit := flags.String("commit", "", "full lowercase source commit SHA")
	dir := flags.String("dir", "dist", "release assets directory")
	tf := flags.String("terraform", "terraform", "Terraform executable for native smoke")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if err := identityFormat(*tag, *commit); err != nil {
		return err
	}
	version := strings.TrimPrefix(*tag, "v")
	switch args[0] {
	case "identity":
		for _, args := range [][]string{{"rev-parse", "HEAD"}, {"rev-parse", "--verify", "refs/tags/" + *tag + "^{commit}"}} {
			out, err := command(nil, "git", args...)
			if err != nil || strings.TrimSpace(string(out)) != *commit {
				return errors.New("HEAD and the existing tag must both match the requested commit")
			}
		}
		branches, err := command(nil, "git", "for-each-ref", "--format=%(refname)", "refs/heads/"+*tag, "refs/remotes/origin/"+*tag)
		if err != nil {
			return err
		}
		if len(bytes.TrimSpace(branches)) != 0 {
			return errors.New("a branch must not have the release tag name")
		}
		if _, err := command(nil, "git", "merge-base", "--is-ancestor", *commit, "refs/remotes/origin/main"); err != nil {
			return errors.New("the release commit must already be merged into origin/main")
		}
		out, err := command(nil, "git", "status", "--porcelain")
		if err != nil || len(bytes.TrimSpace(out)) != 0 {
			return errors.New("release checkout must be clean")
		}
		changes, err := os.ReadFile("CHANGELOG.md")
		if err != nil || !strings.Contains(strings.ReplaceAll(string(changes), "\r\n", "\n"), "\n## "+version+"\n") {
			return errors.New("CHANGELOG.md must have a section headed '## " + version + "'")
		}
		return nil
	case "materials":
		return materials(*commit, version)
	case "verify":
		return verify(*dir, version, *commit)
	case "smoke":
		if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
			return errors.New("packaged-provider runtime checks require native Linux or macOS")
		}
		if err := verify(*dir, version, *commit); err != nil {
			return err
		}
		return smoke(*dir, version, *tf)
	default:
		return errors.New("unknown release preparation command")
	}
}

func identityFormat(tag, commit string) error {
	match := tagPattern.FindStringSubmatch(tag)
	if match == nil || !commitPattern.MatchString(commit) {
		return errors.New("an experimental SemVer prerelease tag and a full lowercase commit SHA are required")
	}
	for _, part := range strings.Split(match[4], ".") {
		if len(part) > 1 && part[0] == '0' && strings.Trim(part, "0123456789") == "" {
			return errors.New("numeric prerelease identifiers cannot have leading zeroes")
		}
	}
	return nil
}

func cleanEnv() []string {
	var env []string
	for _, item := range os.Environ() {
		key := strings.ToUpper(strings.SplitN(item, "=", 2)[0])
		if strings.HasPrefix(key, "IRU_") || strings.HasPrefix(key, "IRUCTL_") || strings.HasPrefix(key, "TF_") ||
			strings.Contains(key, "TOKEN") || strings.Contains(key, "SECRET") || strings.Contains(key, "PASSPHRASE") ||
			strings.HasPrefix(key, "GPG_") || key == "GNUPGHOME" || key == "GOOS" || key == "GOARCH" || key == "GOFLAGS" ||
			key == "CGO_ENABLED" || key == "GOTOOLCHAIN" || key == "CHECKPOINT_DISABLE" {
			continue
		}
		env = append(env, item)
	}
	return append(env, "CGO_ENABLED=0", "GOTOOLCHAIN=local", "CHECKPOINT_DISABLE=1")
}

func command(extra []string, program string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Env = append(cleanEnv(), extra...)
	cmd.Stderr = os.Stderr
	return cmd.Output()
}

type moduleInfo struct {
	Path, Version, Dir string
	Main               bool
	Replace            *moduleInfo
}

// Collect all named legal files, including nested notices, for the union of
// packages actually linked on the four targets. No test/tool dependencies ship.
// This collection is a review aid, not a license-compliance certification.
func materials(commit, version string) error {
	if err := createLegalDirectory("release-legal"); err != nil {
		return fmt.Errorf("use a fresh checkout without release-legal: %w", err)
	}
	modules := map[string]moduleInfo{}
	for _, target := range targets {
		parts := strings.Split(target, "_")
		out, err := command([]string{"GOOS=" + parts[0], "GOARCH=" + parts[1]}, "go", "list", "-mod=readonly", "-deps", "-json", ".")
		if err != nil {
			return err
		}
		decoder := json.NewDecoder(bytes.NewReader(out))
		for {
			var pkg struct{ Module *moduleInfo }
			if err := decoder.Decode(&pkg); err == io.EOF {
				break
			} else if err != nil {
				return err
			}
			if pkg.Module == nil || pkg.Module.Main {
				continue
			}
			m := *pkg.Module
			if m.Replace != nil || m.Version == "" || m.Dir == "" {
				return fmt.Errorf("unversioned or replaced release dependency: %s", m.Path)
			}
			modules[m.Path] = m
		}
	}
	keys := make([]string, 0, len(modules))
	for key := range modules {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var source strings.Builder
	fmt.Fprintf(&source, "Iru Terraform provider %s\nSource commit: %s\n\nOriginal covered source is MPL-2.0; see LICENSE.\nExact source: https://%s/tree/%s\nSource archive: https://%s/archive/%s.tar.gz\n\nDependency source archives (Go module proxy, exact versions):\n", version, commit, module, commit, module, commit)
	for _, key := range keys {
		m := modules[key]
		if err := collectLegal(m.Dir, filepath.Join("release-legal", "modules", filepath.FromSlash(m.Path+"@"+m.Version))); err != nil {
			return err
		}
		fmt.Fprintf(&source, "%s %s https://proxy.golang.org/%s/@v/%s.zip\n", m.Path, m.Version, escapeModule(m.Path), escapeModule(m.Version))
	}
	if err := collectLegal(runtime.GOROOT(), filepath.Join("release-legal", "go")); err != nil {
		return err
	}
	fmt.Fprintf(&source, "\nGo runtime: %s\nGo runtime source: https://go.dev/dl/%s.src.tar.gz\n\nThe repository must be public and these exact source locations accessible before publication.\nThis experimental build has not been validated against a real Iru tenant.\n", runtime.Version(), runtime.Version())
	return os.WriteFile(filepath.Join("release-legal", "SOURCE.txt"), []byte(source.String()), 0644)
}

func createLegalDirectory(dir string) error {
	if err := os.Mkdir(dir, 0755); err != nil {
		return err
	}
	// Git ignores do not affect `go ./...`. A nested module also protects
	// developer checks if future reviewed material needs to retain Go source.
	return os.WriteFile(filepath.Join(dir, "go.mod"), []byte(legalModuleBoundary), 0644)
}

func escapeModule(value string) string {
	var escaped strings.Builder
	for _, r := range value {
		if r >= 'A' && r <= 'Z' {
			escaped.WriteRune('!')
			escaped.WriteRune(r + ('a' - 'A'))
		} else {
			escaped.WriteRune(r)
		}
	}
	return escaped.String()
}

func collectLegal(root, output string) error {
	count := 0
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !isLegalMaterial(entry.Name()) {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink legal file requires review: %s", name)
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		destination := filepath.Join(output, relative)
		content, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
			return err
		}
		count++
		return os.WriteFile(destination, content, 0644)
	})
	if err == nil && count == 0 {
		return fmt.Errorf("no license/notice material found: %s", root)
	}
	return err
}

func isLegalMaterial(name string) bool {
	base := strings.ToUpper(name)
	// A legal-looking prefix on source code is not a standalone notice; in
	// particular Go's copyright_test.go must not enter the generated tree.
	switch strings.ToLower(filepath.Ext(name)) {
	case ".go", ".c", ".cc", ".cpp", ".cxx", ".h", ".hh", ".hpp", ".hxx", ".s", ".asm",
		".py", ".pyi", ".js", ".mjs", ".cjs", ".ts", ".tsx", ".jsx", ".rs", ".java", ".kt", ".kts",
		".swift", ".rb", ".sh", ".bash", ".zsh", ".fish", ".ps1", ".psm1", ".bat", ".cmd", ".proto":
		return false
	}
	for _, prefix := range []string{"LICENSE", "LICENCE", "COPYING", "COPYRIGHT", "NOTICE", "PATENTS", "AUTHORS"} {
		if base == prefix || strings.HasPrefix(base, prefix+".") || strings.HasPrefix(base, prefix+"-") || strings.HasPrefix(base, prefix+"_") {
			return true
		}
	}
	return false
}

func assetPrefix(version string) string { return project + "_" + version }

func verify(dir, version, commit string) error {
	prefix := assetPrefix(version)
	want := map[string]bool{prefix + "_manifest.json": true}
	for _, target := range targets {
		want[prefix+"_"+target+".zip"] = true
	}
	raw, err := os.ReadFile(filepath.Join(dir, prefix+"_SHA256SUMS"))
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || !want[fields[1]] || seen[fields[1]] || len(fields[0]) != 64 {
			return errors.New("checksum file must list exactly four target ZIPs and the versioned manifest, without duplicates")
		}
		digest, err := hex.DecodeString(fields[0])
		if err != nil {
			return errors.New("invalid SHA256 encoding")
		}
		data, err := os.ReadFile(filepath.Join(dir, fields[1]))
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		if !bytes.Equal(digest, sum[:]) {
			return fmt.Errorf("checksum mismatch: %s", fields[1])
		}
		seen[fields[1]] = true
	}
	if len(seen) != len(want) {
		return errors.New("incomplete release checksum list")
	}
	manifest, err := os.ReadFile(filepath.Join(dir, prefix+"_manifest.json"))
	if err != nil {
		return err
	}
	var protocol struct {
		Version  int `json:"version"`
		Metadata struct {
			Protocols []string `json:"protocol_versions"`
		} `json:"metadata"`
	}
	if json.Unmarshal(manifest, &protocol) != nil || protocol.Version != 1 || len(protocol.Metadata.Protocols) != 1 || protocol.Metadata.Protocols[0] != "6.0" {
		return errors.New("release manifest must declare protocol 6.0")
	}
	for _, target := range targets {
		if err := verifyZIP(filepath.Join(dir, prefix+"_"+target+".zip"), version, commit, target); err != nil {
			return err
		}
	}
	return nil
}

func verifyZIP(name, version, commit, target string) error {
	archive, err := zip.OpenReader(name)
	if err != nil {
		return err
	}
	defer archive.Close()
	seen := map[string]bool{}
	regular := map[string]bool{}
	for _, file := range archive.File {
		member := file.Name
		if file.FileInfo().IsDir() {
			member = strings.TrimSuffix(member, "/")
		}
		if !fs.ValidPath(member) || path.Clean(member) != member || strings.Contains(member, "\\") || seen[member] || file.Mode()&os.ModeSymlink != 0 {
			return errors.New("unsafe or duplicate archive member")
		}
		seen[member] = true
		if file.FileInfo().IsDir() {
			continue
		}
		if !file.Mode().IsRegular() {
			return errors.New("special archive member requires review")
		}
		regular[file.Name] = true
		if file.UncompressedSize64 > 100<<20 {
			return errors.New("oversized archive member")
		}
		if file.Name == project+"_v"+version {
			if file.Mode()&0111 == 0 {
				return errors.New("provider binary must be executable in its ZIP")
			}
			content, err := readZIP(file)
			if err != nil {
				return err
			}
			info, err := buildinfo.Read(bytes.NewReader(content))
			if err != nil || info.GoVersion != "go1.27.3" || info.Main.Path != module {
				return errors.New("provider build metadata does not match the pinned release toolchain/module")
			}
			settings := map[string]string{}
			for _, setting := range info.Settings {
				settings[setting.Key] = setting.Value
			}
			// Go omits linker flags from build metadata with -trimpath. Check
			// the version marker as well as the settings it does preserve; the
			// reviewed GoReleaser config owns the main.version linker binding.
			if settings["GOOS"]+"_"+settings["GOARCH"] != target || settings["CGO_ENABLED"] != "0" || settings["-trimpath"] != "true" || !bytes.Contains(content, []byte(version)) {
				return errors.New("provider binary has incorrect target, build flags, or embedded release version")
			}
		} else if file.Name == "release-legal/SOURCE.txt" {
			content, err := readZIP(file)
			if err != nil || !bytes.Contains(content, []byte("Source commit: "+commit+"\n")) {
				return errors.New("source availability notice must identify the exact source commit")
			}
		} else if file.Name != "LICENSE" && !strings.HasPrefix(file.Name, "release-legal/") {
			return errors.New("unexpected archive payload")
		}
	}
	for _, required := range []string{project + "_v" + version, "LICENSE", "release-legal/go.mod", "release-legal/SOURCE.txt", "release-legal/go/LICENSE", "release-legal/modules/github.com/hashicorp/terraform-plugin-framework@v1.19.0/LICENSE"} {
		if !regular[required] {
			return fmt.Errorf("archive is missing %s", required)
		}
	}
	return nil
}

func readZIP(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(io.LimitReader(reader, 100<<20))
}

func smoke(dir, version, terraform string) error {
	if err := os.MkdirAll(".cache", 0700); err != nil {
		return err
	}
	work, err := os.MkdirTemp(".cache", "release-smoke-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	work, err = filepath.Abs(work)
	if err != nil {
		return err
	}
	mirror := filepath.Join(work, "mirror")
	packageDir := filepath.Join(mirror, filepath.FromSlash(address))
	if err := os.MkdirAll(packageDir, 0755); err != nil {
		return err
	}
	name := assetPrefix(version) + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".zip"
	content, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(packageDir, name), content, 0644); err != nil {
		return err
	}
	config := fmt.Sprintf("provider_installation { filesystem_mirror { path = %q } }\n", filepath.ToSlash(mirror))
	configPath := filepath.Join(work, "terraform.tfrc")
	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		return err
	}
	hcl := fmt.Sprintf("terraform {\n required_providers {\n  iru = {\n   source = \"nicholas-r-king/iru\"\n   version = %q\n  }\n }\n}\nprovider \"iru\" {}\n", "= "+version)
	if err := os.WriteFile(filepath.Join(work, "main.tf"), []byte(hcl), 0600); err != nil {
		return err
	}
	env := []string{"TF_CLI_CONFIG_FILE=" + configPath, "TF_IN_AUTOMATION=1", "TF_INPUT=0", "IRU_API_URL=https://synthetic.api.iru.com", "IRU_API_TOKEN=synthetic-release-token"}
	if _, err := command(env, terraform, "-chdir="+work, "init", "-backend=false", "-input=false"); err != nil {
		return err
	}
	raw, err := command(env, terraform, "-chdir="+work, "providers", "schema", "-json")
	if err != nil {
		return err
	}
	var schema struct {
		Providers map[string]json.RawMessage `json:"provider_schemas"`
	}
	if json.Unmarshal(raw, &schema) != nil || len(schema.Providers[address]) == 0 {
		return errors.New("packaged provider did not export its schema through Terraform")
	}
	fmt.Printf("Native package install and schema export passed for %s/%s; no live tenant was contacted.\n", runtime.GOOS, runtime.GOARCH)
	return nil
}
