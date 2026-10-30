// SPDX-License-Identifier: MPL-2.0

package main

import (
	"archive/zip"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestReleaseConfigurationCIHasNoSigningOrWriteAccess(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "test.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := strings.ReplaceAll(string(raw), "\r\n", "\n")
	jobPattern := regexp.MustCompile(`(?ms)^  release-config:\n(.*?)(?:^  [A-Za-z0-9_-]+:\n|\z)`)
	match := jobPattern.FindStringSubmatch(workflow)
	if match == nil {
		t.Fatal("normal PR/main CI must validate the release configuration")
	}
	job := match[1]
	for _, forbidden := range []string{"secrets.", "write", "environment:", "GPG_", "PASSPHRASE", "GITHUB_TOKEN", "GH_TOKEN", "args:"} {
		if strings.Contains(job, forbidden) {
			t.Fatalf("release configuration CI must not acquire signing/publishing access: %s", forbidden)
		}
	}
	for _, required := range []string{"permissions:\n      contents: read\n", "ref: ${{ github.sha }}", "persist-credentials: false", "goreleaser/goreleaser-action@f06c13b6b1a9625abc9e6e439d9c05a8f2190e94", "version: v2.19.0", "install-only: true"} {
		if !strings.Contains(job, required) {
			t.Fatalf("release configuration CI is missing a required isolation/pinning setting: %s", required)
		}
	}
	runs := regexp.MustCompile(`(?m)^\s+run:\s*(.+)$`).FindAllStringSubmatch(job, -1)
	if len(runs) != 1 || strings.TrimSpace(runs[0][1]) != "goreleaser check" {
		t.Fatal("release configuration CI may execute only goreleaser check")
	}
}

// A real trimmed Go executable exercises the metadata reader: linker flags are
// absent from Go's build metadata when -trimpath is enabled. Never run the fixture.
func TestTrimmedReleaseBinaryMetadata(t *testing.T) {
	if runtime.Version() != "go1.27.3" {
		t.Skip("release binaries use the pinned Go 1.27.3 toolchain")
	}
	dir := t.TempDir()
	for name, content := range map[string]string{
		"go.mod":  "module " + module + "\n\ngo 1.25.0\n",
		"main.go": "package main\nimport \"fmt\"\nvar version = \"dev\"\nfunc main() { fmt.Print(version) }\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	binary := filepath.Join(dir, "synthetic-provider")
	cmd := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-mod=readonly", "-ldflags=-s -w -X main.version=0.1.0-alpha.1", "-o", binary, ".")
	cmd.Dir = dir
	cmd.Env = append(cleanEnv(), "GOWORK=off")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("synthetic binary build failed: %v\n%s", err, output)
	}
	content, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("a", 40)
	writePackage := func(version string) string {
		t.Helper()
		name := filepath.Join(dir, "synthetic-"+version+".zip")
		file, err := os.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		archive := zip.NewWriter(file)
		for name, data := range map[string][]byte{
			project + "_v" + version:   content,
			"LICENSE":                  []byte("synthetic license"),
			"release-legal/go.mod":     []byte(legalModuleBoundary),
			"release-legal/SOURCE.txt": []byte("Source commit: " + sha + "\n"),
			"release-legal/go/LICENSE": []byte("synthetic runtime notice"),
			"release-legal/modules/github.com/hashicorp/terraform-plugin-framework@v1.19.0/LICENSE": []byte("synthetic framework notice"),
		} {
			header := &zip.FileHeader{Name: name, Method: zip.Deflate}
			header.SetMode(0644)
			if name == project+"_v"+version {
				header.SetMode(0755)
			}
			writer, err := archive.CreateHeader(header)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := writer.Write(data); err != nil {
				t.Fatal(err)
			}
		}
		if err := archive.Close(); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		return name
	}
	target := runtime.GOOS + "_" + runtime.GOARCH
	name := writePackage("0.1.0-alpha.1")
	if err := verifyZIP(name, "0.1.0-alpha.1", sha, target); err != nil {
		t.Fatal(err)
	}
	if verifyZIP(name, "0.1.0-alpha.1", sha, "unrelated_arch") == nil || verifyZIP(name, "0.1.0-alpha.1", strings.Repeat("b", 40), target) == nil {
		t.Fatal("incorrect binary target or source notice accepted")
	}
	if verifyZIP(writePackage("0.2.0-alpha.1"), "0.2.0-alpha.1", sha, target) == nil {
		t.Fatal("incorrect embedded version marker accepted")
	}
}

func TestExperimentalIdentity(t *testing.T) {
	sha := strings.Repeat("a", 40)
	for _, tag := range []string{"v0.1.0-alpha.1", "v2.0.0-rc.0", "v1.2.3-abc-123"} {
		if err := identityFormat(tag, sha); err != nil {
			t.Fatal(err)
		}
	}
	for _, tag := range []string{"v0.1.0", "0.1.0-alpha", "v01.1.0-alpha", "v0.1.0-alpha.01", "v0.1.0-alpha+meta", "v0.1.0-", "v0.1.0-alpha/other"} {
		if identityFormat(tag, sha) == nil {
			t.Fatalf("unsafe or nonexperimental tag accepted: %s", tag)
		}
	}
	if identityFormat("v0.1.0-alpha.1", "HEAD") == nil {
		t.Fatal("symbolic source ref accepted")
	}
}

func TestArchiveRejectsTraversalAndDuplicates(t *testing.T) {
	for _, members := range [][]string{{"../outside"}, {"/outside"}, {"release-legal\\outside"}, {"LICENSE", "LICENSE"}, {"terraform-provider-unrelated_v0.1.0-alpha.1"}, {"terraform-provider-iru_v0.1.0-alpha.1/", "LICENSE/", "release-legal/go.mod/", "release-legal/SOURCE.txt/", "release-legal/go/LICENSE/", "release-legal/modules/github.com/hashicorp/terraform-plugin-framework@v1.19.0/LICENSE/"}} {
		name := filepath.Join(t.TempDir(), "synthetic.zip")
		file, err := os.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		archive := zip.NewWriter(file)
		for _, member := range members {
			writer, err := archive.Create(member)
			if err != nil {
				t.Fatal(err)
			}
			// ZIP directory entries have no body. Keep valid empty directory
			// placeholders so the verifier, rather than the fixture writer,
			// rejects substituting directories for required release files.
			if strings.HasSuffix(member, "/") {
				continue
			}
			if _, err := writer.Write([]byte("synthetic")); err != nil {
				t.Fatal(err)
			}
		}
		if err := archive.Close(); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		if verifyZIP(name, "0.1.0-alpha.1", strings.Repeat("a", 40), "linux_amd64") == nil {
			t.Fatalf("invalid archive accepted: %v", members)
		}
	}
}

func TestChecksumWhitelistFailsClosed(t *testing.T) {
	for _, sums := range []string{"", strings.Repeat("a", 64) + "  ../outside\n", strings.Repeat("a", 64) + "  terraform-provider-iru_0.1.0-alpha.1_linux_amd64.zip\n"} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "terraform-provider-iru_0.1.0-alpha.1_SHA256SUMS"), []byte(sums), 0600); err != nil {
			t.Fatal(err)
		}
		if verify(dir, "0.1.0-alpha.1", strings.Repeat("a", 40)) == nil {
			t.Fatal("incomplete or unsafe checksum list accepted")
		}
	}
}

func TestModuleProxyEscapingAndNestedNotices(t *testing.T) {
	if escapeModule("github.com/Example/Lib") != "github.com/!example/!lib" {
		t.Fatal("incorrect Go module proxy escaping")
	}
	source := t.TempDir()
	if err := os.Mkdir(filepath.Join(source, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"LICENSE", "NOTICE.txt", "nested/COPYRIGHT", "nested/LICENSE-BSD"} {
		if err := os.WriteFile(filepath.Join(source, file), []byte(file), 0600); err != nil {
			t.Fatal(err)
		}
	}
	output := t.TempDir()
	if err := collectLegal(source, output); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"LICENSE", "NOTICE.txt", "nested/COPYRIGHT", "nested/LICENSE-BSD"} {
		data, err := os.ReadFile(filepath.Join(output, file))
		if err != nil || string(data) != file {
			t.Fatalf("notice omitted or changed: %s", file)
		}
	}
	if collectLegal(t.TempDir(), t.TempDir()) == nil {
		t.Fatal("missing legal material accepted")
	}
}

func TestLegalMaterialExcludesSourceCode(t *testing.T) {
	source, output := t.TempDir(), t.TempDir()
	notices := []string{"LICENSE", "LICENSE.txt", "LICENSE-MIT", "LICENSE.Apache-2.0", "COPYING.LESSER", "NOTICE.md", "copyright.txt", "PATENTS", "AUTHORS"}
	sources := []string{"copyright_test.go", "LICENSE.go", "NOTICE.generated.go", "LICENSE_test.py", "COPYRIGHT.h", "AUTHORS.rs", "NOTICE.sh", "LICENCE.ts"}
	for _, name := range append(append([]string{}, notices...), sources...) {
		if err := os.WriteFile(filepath.Join(source, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := collectLegal(source, output); err != nil {
		t.Fatal(err)
	}
	for _, name := range notices {
		if _, err := os.Stat(filepath.Join(output, name)); err != nil {
			t.Fatalf("standalone legal notice was omitted: %s", name)
		}
	}
	for _, name := range sources {
		if _, err := os.Stat(filepath.Join(output, name)); !os.IsNotExist(err) {
			t.Fatalf("source code was mistaken for a standalone notice: %s", name)
		}
	}
}

func TestGeneratedLegalTreeIsOutsideGoPackageTraversal(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{
		"go.mod":  "module example.invalid/synthetic-provider\n\ngo 1.25.0\n",
		"main.go": "package main\nfunc main() {}\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	legal := filepath.Join(root, "release-legal")
	if err := createLegalDirectory(legal); err != nil {
		t.Fatal(err)
	}
	// Even a future retained source notice must not become a provider package.
	if err := os.WriteFile(filepath.Join(legal, "copyright_test.go"), []byte("intentionally invalid Go source\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "list", "./...")
	cmd.Dir = root
	cmd.Env = append(cleanEnv(), "GOWORK=off", "GOPROXY=off")
	output, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != "example.invalid/synthetic-provider" {
		t.Fatalf("Go traversed generated legal material: %v\n%s", err, output)
	}
}
