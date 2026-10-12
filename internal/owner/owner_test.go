package owner_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/readeem/omihomo/internal/owner"
)

func TestStoreThenLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owner")
	if err := owner.Store(path, 1000); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode())
	}
	if uid, err := owner.Load(path); err != nil || uid != 1000 {
		t.Fatalf("load = %d, %v", uid, err)
	}
	if err := owner.Store(path, 1001); err != nil {
		t.Fatal(err)
	}
	if uid, _ := owner.Load(path); uid != 1001 {
		t.Fatalf("replaced owner = %d", uid)
	}
}

func TestUnclaimed(t *testing.T) {
	if _, err := owner.Load(filepath.Join(t.TempDir(), "owner")); !errors.Is(err, owner.ErrUnclaimed) {
		t.Fatalf("err = %v", err)
	}
}

func TestUntrustedFilesAreRejected(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string, mode os.FileMode) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
		os.Chmod(p, mode)
		return p
	}
	real := write("real", "1000\n", 0o600)
	link := filepath.Join(dir, "link")
	os.Symlink(real, link)
	for name, path := range map[string]string{
		"group-writable": write("gw", "1000\n", 0o620),
		"world-writable": write("ww", "1000\n", 0o606),
		"symlink":        link,
		"directory":      dir,
		"not a number":   write("nan", "root\n", 0o600),
		"negative":       write("neg", "-1\n", 0o600),
		"empty":          write("empty", "", 0o600),
	} {
		if _, err := owner.Load(path); err == nil || errors.Is(err, owner.ErrUnclaimed) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}
