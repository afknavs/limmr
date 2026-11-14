package health

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"
)

// Capture every goroutine before exit without relying on the main loop or DB.
func writeStuckStacks(home string, now time.Time) error {
	dir := filepath.Join(home, "logs")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	buf := make([]byte, 64<<10)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]byte, len(buf)*2)
	}
	name := filepath.Join(dir, "engine-stuck-"+now.UTC().Format("20060102T150405.000000000Z")+".txt")
	f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(buf)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	names, err := filepath.Glob(filepath.Join(dir, "engine-stuck-*.txt"))
	if err != nil {
		return err
	}
	sort.Strings(names) // fixed-width UTC timestamps sort oldest first
	for _, old := range names[:max(0, len(names)-3)] {
		if err := os.Remove(old); err != nil {
			return err
		}
	}
	return nil
}
