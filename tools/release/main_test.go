package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestArchiveIsDeterministicAndContainsNotices(t *testing.T) {
	dir := t.TempDir()
	files := map[string][]byte{"koard": []byte("binary"), "LICENSE": []byte("MIT"), "NOTICE": []byte("upstream"), "INSTALL.md": []byte("instructions")}
	a, b := filepath.Join(dir, "a.tar.gz"), filepath.Join(dir, "b.tar.gz")
	for _, path := range []string{a, b} {
		if err := archive(path, files); err != nil {
			t.Fatal(err)
		}
	}
	first, _ := os.ReadFile(a)
	second, _ := os.ReadFile(b)
	if !bytes.Equal(first, second) {
		t.Fatal("archive bytes differ")
	}
	gz, err := gzip.NewReader(bytes.NewReader(first))
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for _, name := range []string{"INSTALL.md", "LICENSE", "NOTICE", "koard"} {
		h, err := tr.Next()
		if err != nil {
			t.Fatal(err)
		}
		mode := int64(0644)
		if name == "koard" {
			mode = 0755
		}
		data, err := io.ReadAll(tr)
		if err != nil || h.Name != name || h.Mode != mode || h.Uid != 0 || h.Gid != 0 || h.ModTime.Unix() != 0 || !bytes.Equal(data, files[name]) {
			t.Fatalf("invalid archive entry: %+v, %v", h, err)
		}
	}
	if _, err := tr.Next(); err != io.EOF {
		t.Fatalf("unexpected entry: %v", err)
	}
}

func TestReleaseUsageAndExistingOutput(t *testing.T) {
	if err := run(nil); err == nil {
		t.Fatal("missing output accepted")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "preserve")
	if err := os.WriteFile(marker, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{dir}); err == nil {
		t.Fatal("existing output accepted")
	}
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != "existing" {
		t.Fatal("existing output changed")
	}
}

func TestGoLicenseLayouts(t *testing.T) {
	for _, name := range []string{"upstream", "homebrew", "unrelated-parent"} {
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			root := filepath.Join(parent, "go")
			if name == "homebrew" {
				root = filepath.Join(parent, "libexec")
			}
			if err := os.Mkdir(root, 0755); err != nil {
				t.Fatal(err)
			}
			want := filepath.Join(root, "LICENSE")
			if name != "upstream" {
				want = filepath.Join(parent, "LICENSE")
			}
			if err := os.WriteFile(want, []byte("Go license"), 0644); err != nil {
				t.Fatal(err)
			}
			got, err := goLicensePath(root)
			if name == "unrelated-parent" {
				if err == nil {
					t.Fatal("accepted unrelated parent license")
				}
			} else if err != nil || got != want {
				t.Fatalf("got %q, %v; want %q", got, err, want)
			}
		})
	}
}
