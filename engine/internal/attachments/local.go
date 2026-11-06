package attachments

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// ReadLocal walks from the home directory using no-follow directory descriptors.
// Refusing all symlinks avoids races and prevents an agent escaping the boundary.
func ReadLocal(raw, home, limmrHome string) ([]byte, string, error) {
	if strings.HasPrefix(raw, "~/") {
		raw = filepath.Join(home, raw[2:])
	}
	if !filepath.IsAbs(raw) {
		return nil, "", fmt.Errorf("Use an absolute path or a path starting with ~/.")
	}
	home, err := filepath.Abs(home)
	if err != nil {
		return nil, "", err
	}
	raw = filepath.Clean(raw)
	rel, err := filepath.Rel(home, raw)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return nil, "", fmt.Errorf("Choose a regular file inside your home folder.")
	}
	for _, protected := range []string{limmrHome, filepath.Join(home, "Library", "Application Support", "Limmr")} {
		if protected == "" {
			continue
		}
		lh, err := filepath.Abs(protected)
		if err != nil {
			return nil, "", err
		}
		lr, err := filepath.Rel(lh, raw)
		if err == nil && lr != ".." && !strings.HasPrefix(lr, ".."+string(os.PathSeparator)) {
			return nil, "", fmt.Errorf("Choose a file outside Limmr's data folder.")
		}
	}
	parts := strings.Split(rel, string(os.PathSeparator))
	for i, part := range parts {
		p := strings.ToLower(part)
		if p == ".ssh" || p == ".aws" || p == ".gnupg" || p == ".config" || p == ".env" || strings.HasPrefix(p, ".env.") || strings.HasPrefix(p, "id_rsa") || strings.HasPrefix(p, "id_ed25519") || strings.HasPrefix(p, "id_ecdsa") || strings.HasPrefix(p, ".claude") || p == ".codex" || p == "credentials" || (i > 0 && strings.EqualFold(parts[i-1], "Library") && p == "keychains") || strings.Contains(p, "token") || strings.Contains(p, "secret") || strings.Contains(p, "password") || strings.Contains(p, "private_key") || strings.HasSuffix(p, ".key") || strings.HasSuffix(p, ".pem") || strings.HasSuffix(p, ".p12") || strings.HasSuffix(p, ".pfx") {
			return nil, "", fmt.Errorf("This path may contain credentials. Choose a different file.")
		}
	}
	fd, err := unix.Open(home, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, "", fmt.Errorf("Cannot open your home folder; choose another file.")
	}
	for i, part := range parts {
		flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
		if i < len(parts)-1 {
			flags |= unix.O_DIRECTORY
		}
		next, e := unix.Openat(fd, part, flags, 0)
		unix.Close(fd)
		if e != nil {
			return nil, "", fmt.Errorf("Cannot open this file safely. Choose a regular file without symlinks.")
		}
		fd = next
	}
	f := os.NewFile(uintptr(fd), "attachment")
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, "", fmt.Errorf("Choose a regular file.")
	}
	header := make([]byte, 512)
	n, _ := f.ReadAt(header, 0)
	if err := CheckSize(info.Size(), MIME(header[:n], raw)); err != nil {
		return nil, "", err
	}
	b, err := io.ReadAll(io.LimitReader(f, MaxFetch+1))
	if err != nil || len(b) == 0 || len(b) > MaxFetch {
		return nil, "", fmt.Errorf("This file is empty, unavailable, or exceeds 100 MiB. Send a link instead. Choose another file or send a link.")
	}
	return b, Name(filepath.Base(raw)), nil
}
