// Command release builds offline release archives from a clean Koard checkout.
package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/koard-ai/koard/internal/cli"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func output(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local", "GOWORK=off", "GOFLAGS=")
	b, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %w: %s", name, err, b)
	}
	return strings.TrimSpace(string(b)), nil
}

func goLicensePath(root string) (string, error) {
	path := filepath.Join(root, "LICENSE")
	if _, err := os.Stat(path); os.IsNotExist(err) && filepath.Base(root) == "libexec" {
		// Homebrew keeps distribution notices beside, rather than inside, libexec.
		path = filepath.Join(filepath.Dir(root), "LICENSE")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("Go license is not a regular file: %s", path)
	}
	return path, nil
}

func run(args []string) (err error) {
	if len(args) != 1 {
		return fmt.Errorf("usage: go run ./tools/release <new-output-directory> (from repository root)")
	}
	dir := args[0]
	if _, err := os.Lstat(dir); err == nil {
		return fmt.Errorf("output already exists: %s", dir)
	} else if !os.IsNotExist(err) {
		return err
	}
	status, err := output("git", "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return err
	}
	if status != "" {
		return fmt.Errorf("release requires a clean checkout")
	}
	commit, err := output("git", "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	toolchain, err := output("go", "version")
	if err != nil {
		return err
	}
	if !strings.Contains(toolchain, " "+runtime.Version()+" ") {
		return fmt.Errorf("packager and build toolchains differ")
	}
	yaml, err := output("go", "list", "-m", "-f", "{{.Dir}}", "go.yaml.in/yaml/v4")
	if err != nil {
		return err
	}
	goLicense, err := goLicensePath(runtime.GOROOT())
	if err != nil {
		return err
	}
	files := map[string][]byte{}
	for name, path := range map[string]string{
		"LICENSE": "LICENSE", "INSTALL.md": "docs/releases/0.1.0.md",
		"licenses/engram-MIT.txt":       "internal/adapters/opencode/engramassets/LICENSE",
		"licenses/engram-provenance.md": "internal/adapters/opencode/engramassets/README.md",
		"licenses/yaml-Apache-2.0.txt":  filepath.Join(yaml, "LICENSE"),
		"licenses/yaml-NOTICE.txt":      filepath.Join(yaml, "NOTICE"),
		"licenses/go-BSD.txt":           goLicense,
		"licenses/go-PATENTS.txt":       filepath.Join(runtime.GOROOT(), "PATENTS"),
	} {
		files[name], err = os.ReadFile(path)
		if err != nil {
			return err
		}
	}
	if err = os.Mkdir(dir, 0755); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(dir)
		}
	}()
	var sums strings.Builder
	for _, target := range []string{"darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64"} {
		parts := strings.Split(target, "/")
		binary := filepath.Join(dir, "koard")
		cmd := exec.Command("go", "build", "-mod=readonly", "-trimpath", "-buildvcs=false", "-o", binary, "./cmd/koard")
		cmd.Env = append(os.Environ(), "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local", "GOWORK=off", "GOFLAGS=", "GOEXPERIMENT=", "CGO_ENABLED=0", "GOOS="+parts[0], "GOARCH="+parts[1], "GOAMD64=v1", "GOARM64=v8.0")
		if b, e := cmd.CombinedOutput(); e != nil {
			return fmt.Errorf("build %s: %w: %s", target, e, b)
		}
		files["koard"], err = os.ReadFile(binary)
		if err != nil {
			return err
		}
		files["BUILD.txt"] = []byte(fmt.Sprintf("Koard %s\nSource: https://github.com/gasparnd/koard-ai\nCommit: %s\nToolchain: %s\nTarget: %s\nCGO_ENABLED=0 GOAMD64=v1 GOARM64=v8.0 GOEXPERIMENT=\nFlags: -mod=readonly -trimpath -buildvcs=false\nDependency: go.yaml.in/yaml/v4 v4.0.0 (see go.sum at source commit)\n", cli.Version, commit, toolchain, target))
		name := fmt.Sprintf("koard_%s_%s_%s.tar.gz", cli.Version, parts[0], parts[1])
		if err = archive(filepath.Join(dir, name), files); err != nil {
			return err
		}
		data, e := os.ReadFile(filepath.Join(dir, name))
		if e != nil {
			return e
		}
		fmt.Fprintf(&sums, "%x  %s\n", sha256.Sum256(data), name)
		if err = os.Remove(binary); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(sums.String()), 0644)
}

func archive(path string, files map[string][]byte) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if e := f.Close(); err == nil {
			err = e
		}
	}()
	gz := gzip.NewWriter(f)
	defer func() {
		if e := gz.Close(); err == nil {
			err = e
		}
	}()
	tw := tar.NewWriter(gz)
	defer func() {
		if e := tw.Close(); err == nil {
			err = e
		}
	}()
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		mode := int64(0644)
		if name == "koard" {
			mode = 0755
		}
		h := &tar.Header{Name: name, Mode: mode, Size: int64(len(files[name])), ModTime: time.Unix(0, 0), Format: tar.FormatUSTAR}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if _, err := tw.Write(files[name]); err != nil {
			return err
		}
	}
	return nil
}
