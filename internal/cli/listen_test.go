package cli

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListenRefusesASharedDirectory(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0o777)
	if l, err := listen(filepath.Join(dir, "s"), filepath.Join(dir, "owner")); err == nil {
		l.Close()
		t.Fatal("bound a socket in a directory others can write")
	}
}

func TestListenLeavesALiveDaemonAlone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s")
	first, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if l, err := listen(path, filepath.Join(dir, "owner")); err == nil || !strings.Contains(err.Error(), "another daemon") {
		if l != nil {
			l.Close()
		}
		t.Fatalf("listen over a live socket = %v", err)
	}
}

func TestListenReplacesAStaleSocket(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s")
	stale, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	stale.(*net.UnixListener).SetUnlinkOnClose(false)
	stale.Close()
	l, err := listen(path, filepath.Join(dir, "owner"))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode %v", info.Mode().Perm())
	}
}
