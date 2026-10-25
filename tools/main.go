// SPDX-License-Identifier: MPL-2.0

// Contributor checks use Go's process APIs directly, with no shell dependency.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"time"
)

const docsTool = "github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@v0.25.0"
const providerAddress = "registry.terraform.io/nicholas-r-king/iru"

type runner struct {
	root, goExe, terraform string
	env                    []string
}

func main() {
	if err := execute(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func execute(args []string) error {
	if len(args) == 0 || (args[0] != "verify" && args[0] != "docs") {
		return errors.New("usage: go run ./tools <verify|docs> [-go path] [-terraform path] [-skip-vet-reason reason] [-check-docs] [-check]")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	goPath := flags.String("go", "go", "Go executable (defaults to PATH)")
	tfPath := flags.String("terraform", "", "Terraform executable (defaults to PATH when available)")
	skipVet := flags.String("skip-vet-reason", "", "explicit reason to skip vet; a skip is not a pass")
	checkDocs := flags.Bool("check-docs", false, "verify deterministic documentation during verify")
	check := flags.Bool("check", false, "fail if docs regeneration changes documentation")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	module, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil || !strings.Contains(strings.ReplaceAll(string(module), "\r\n", "\n"), "module github.com/nicholas-r-king/terraform-provider-iru\n") {
		return errors.New("run contributor tools from the provider repository root")
	}
	goExe, err := executable(*goPath)
	if err != nil {
		return fmt.Errorf("Go executable unavailable: %w", err)
	}
	var terraform string
	if *tfPath != "" {
		terraform, err = executable(*tfPath)
		if err != nil {
			return fmt.Errorf("Terraform executable unavailable: %w", err)
		}
	} else {
		terraform, _ = executable("terraform")
	}
	if (args[0] == "docs" || *checkDocs) && terraform == "" {
		return errors.New("documentation checks require Terraform on PATH or -terraform path")
	}
	r := runner{root: root, goExe: goExe, terraform: terraform, env: cleanEnv(os.Environ())}
	if args[0] == "docs" {
		return r.docs(*check)
	}
	return r.verify(*skipVet, *checkDocs)
}

func executable(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", err
	}
	return filepath.Abs(path)
}

// Child-only environment: never read tenant credentials or inherit Terraform
// CLI overrides/debug settings. Native tests/docs must not inherit cross targets.
func cleanEnv(env []string) []string {
	result := []string{}
	for _, entry := range env {
		key := strings.ToUpper(strings.SplitN(entry, "=", 2)[0])
		if strings.HasPrefix(key, "IRU_") || strings.HasPrefix(key, "IRUCTL_") || strings.HasPrefix(key, "TF_") ||
			key == "GOOS" || key == "GOARCH" || key == "CGO_ENABLED" || key == "GOTOOLCHAIN" || key == "CHECKPOINT_DISABLE" {
			continue
		}
		result = append(result, entry)
	}
	return append(result, "GOTOOLCHAIN=local", "CGO_ENABLED=0", "CHECKPOINT_DISABLE=1")
}

func (r runner) run(capture bool, extraEnv []string, bin string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = r.root
	cmd.Env = append(append([]string{}, r.env...), extraEnv...)
	cmd.Stderr = os.Stderr
	var output bytes.Buffer
	if capture {
		cmd.Stdout = &output
	} else {
		cmd.Stdout = os.Stdout
	}
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s failed: %w", filepath.Base(bin), err)
	}
	return output.Bytes(), nil
}

func suffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func (r runner) build() (string, error) {
	dir := filepath.Join(r.root, ".cache", "bin")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	_, err := r.run(false, nil, r.goExe, "build", "-trimpath", "-o", filepath.Join(dir, "terraform-provider-iru"+suffix()), ".")
	return dir, err
}

func (r runner) verify(skipVet string, checkDocs bool) error {
	format, err := r.run(true, nil, filepath.Join(filepath.Dir(r.goExe), "gofmt"+suffix()), "-l", "main.go", "internal", "tools")
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(format)) > 0 {
		return errors.New("Go formatting differs; run gofmt and review the diff")
	}
	if _, err := r.run(false, nil, r.goExe, "mod", "verify"); err != nil {
		return err
	}
	if _, err := r.build(); err != nil {
		return err
	}
	if _, err := r.run(false, []string{"IRU_TEST_TERRAFORM=" + r.terraform}, r.goExe, "test", "-vet=off", "-count=1", "./..."); err != nil {
		return err
	}
	if r.terraform == "" {
		fmt.Println("Terraform CLI test NOT RUN: supply Terraform on PATH or -terraform path.")
	}
	if strings.TrimSpace(skipVet) != "" {
		fmt.Println("Vet NOT RUN:", skipVet)
	} else if _, err := r.run(false, nil, r.goExe, "vet", "./..."); err != nil {
		return err
	}
	if r.terraform != "" {
		if _, err := r.run(false, nil, r.terraform, "fmt", "-check", "-recursive", "examples"); err != nil {
			return err
		}
	}
	if checkDocs {
		if err := r.docs(true); err != nil {
			return err
		}
	}
	fmt.Println("Requested checks completed; skipped checks are not passes.")
	return nil
}

func (r runner) docs(check bool) error {
	before, err := snapshot(filepath.Join(r.root, "docs"))
	if err != nil {
		return err
	}
	bin, err := r.build()
	if err != nil {
		return err
	}
	work := filepath.Join(r.root, ".cache", "docs-work")
	if err := os.MkdirAll(work, 0700); err != nil {
		return err
	}
	cliPath := filepath.Join(work, "terraform.tfrc")
	cli := fmt.Sprintf("provider_installation { dev_overrides { %q = %q } }\n", providerAddress, filepath.ToSlash(bin))
	if err := os.WriteFile(cliPath, []byte(cli), 0600); err != nil {
		return err
	}
	hcl := "terraform {\n required_providers {\n  iru = { source = \"nicholas-r-king/iru\" }\n }\n}\nprovider \"iru\" {}\n"
	if err := os.WriteFile(filepath.Join(work, "main.tf"), []byte(hcl), 0600); err != nil {
		return err
	}
	// Schema export configures no live client and uses only synthetic values.
	env := []string{"TF_CLI_CONFIG_FILE=" + cliPath, "TF_IN_AUTOMATION=1", "TF_INPUT=0", "IRU_API_URL=https://synthetic.api.iru.com", "IRU_API_TOKEN=synthetic-docs-token"}
	raw, err := r.run(true, env, r.terraform, "-chdir="+work, "providers", "schema", "-json")
	if err != nil {
		return err
	}
	aliased, err := aliasSchema(raw)
	if err != nil {
		return err
	}
	schemaPath := filepath.Join(work, "tfplugindocs-schema.json")
	if err := os.WriteFile(schemaPath, aliased, 0600); err != nil {
		return err
	}
	// Pinned official Go tool; no global install or Terraform download.
	args := []string{"run", docsTool, "generate", "--provider-name", "iru", "--providers-schema", schemaPath, "--website-temp-dir", filepath.Join(r.root, ".cache", "tfplugindocs-temp")}
	if _, err := r.run(false, nil, r.goExe, args...); err != nil {
		return err
	}
	if _, err := r.run(false, nil, r.goExe, "run", docsTool, "validate", "--provider-name", "iru", "--providers-schema", schemaPath); err != nil {
		return err
	}
	if check {
		after, err := snapshot(filepath.Join(r.root, "docs"))
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(before, after) {
			return errors.New("generated documentation changed; review and commit the output")
		}
	}
	return nil
}

// tfplugindocs v0.25.0 accepts short names in supplied schemas. Only alias the
// address in this temporary document; preserve the actual provider schema.
func aliasSchema(raw []byte) ([]byte, error) {
	var document map[string]json.RawMessage
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, errors.New("invalid Terraform schema JSON")
	}
	var schemas map[string]json.RawMessage
	if err := json.Unmarshal(document["provider_schemas"], &schemas); err != nil || len(schemas[providerAddress]) == 0 {
		return nil, errors.New("expected Iru provider schema is missing")
	}
	aliased, err := json.Marshal(map[string]json.RawMessage{"iru": schemas[providerAddress]})
	if err != nil {
		return nil, err
	}
	document["provider_schemas"] = aliased
	return json.Marshal(document)
}

func snapshot(dir string) (map[string][32]byte, error) {
	files := map[string][32]byte{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files[path] = sha256.Sum256(data)
		}
		return nil
	})
	return files, err
}
