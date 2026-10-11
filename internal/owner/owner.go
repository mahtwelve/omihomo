// Package owner reads and records the one account the daemon serves.
//
// The install step claims ownership by writing the owner's uid to a file in
// the daemon's state directory. The daemon trusts that file only while it is
// a regular file that the daemon's own account owns and nobody else can
// write, so a world-writable state directory cannot hand the socket to
// another account.
package owner

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// DefaultPath is where the install step records the owner.
const DefaultPath = "/var/lib/omihomo/owner"

// ErrUnclaimed means no owner has been recorded yet.
var ErrUnclaimed = errors.New("no owner has claimed this omihomo install")

// Load returns the owner uid recorded at path.
func Load(path string) (int, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, ErrUnclaimed
	}
	if err != nil {
		return 0, fmt.Errorf("read owner: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0, fmt.Errorf("read owner: %w", err)
	}
	if err := trusted(info); err != nil {
		return 0, fmt.Errorf("owner file %s: %w", path, err)
	}
	raw, err := io.ReadAll(io.LimitReader(f, 32))
	if err != nil {
		return 0, fmt.Errorf("read owner: %w", err)
	}
	uid, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 31)
	if err != nil {
		return 0, fmt.Errorf("owner file %s does not hold a uid", path)
	}
	return int(uid), nil
}

func trusted(info fs.FileInfo) error {
	if !info.Mode().IsRegular() {
		return errors.New("not a regular file")
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("cannot read its ownership")
	}
	if int(st.Uid) != os.Geteuid() {
		return fmt.Errorf("owned by uid %d, not by the daemon's uid %d", st.Uid, os.Geteuid())
	}
	if info.Mode().Perm()&0o022 != 0 {
		return errors.New("writable by group or others")
	}
	return nil
}

// Store records uid as the owner at path, replacing any previous owner.
func Store(path string, uid int) error {
	if uid < 0 {
		return fmt.Errorf("invalid uid %d", uid)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".owner-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(strconv.Itoa(uid) + "\n"); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
