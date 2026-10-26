package health_test

import (
	"bytes"
	"limmr/internal/health"
	"limmr/internal/testkit"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestR4LogCapAfterEveryWriteAndRestart(t *testing.T) {
	home := t.TempDir()
	l, err := health.OpenLog(home)
	testkit.Must(t, err)
	path := filepath.Join(home, "logs", "engine.log")
	before, err := os.Stat(path)
	testkit.Must(t, err)
	start := time.Now()
	large := bytes.Repeat([]byte("x"), 25<<20)
	copy(large[len(large)-8:], "end-tail")
	n, err := l.Write(large)
	testkit.Must(t, err)
	if n != len(large) {
		t.Fatal("short log write")
	}
	after, err := os.Stat(path)
	testkit.Must(t, err)
	if after.Size() > 20<<20 || !os.SameFile(before, after) {
		t.Fatal("oversized final write escaped log cap")
	}
	tail, err := os.ReadFile(path)
	testkit.Must(t, err)
	if len(tail) != 5<<20 || !bytes.HasSuffix(tail, []byte("end-tail")) {
		t.Fatal("rotation lost the final tail")
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 96; i++ {
			if err := health.TrimLog(path, 20<<20, 5<<20); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 24; j++ {
				if _, e := l.Write(bytes.Repeat([]byte("log record\n"), 16000)); e != nil {
					t.Error(e)
					return
				}
			}
		}()
	}
	wg.Wait()
	testkit.Must(t, l.Close())
	l, err = health.OpenLog(home)
	testkit.Must(t, err)
	defer l.Close()
	after, err = os.Stat(path)
	testkit.Must(t, err)
	if after.Size() > 20<<20 || after.Mode().Perm() != 0600 {
		t.Fatal("restart violated private bounded log")
	}
	t.Logf("25MiB + concurrent log writes/rotation/reopen %.3fms; final=%d bytes", float64(time.Since(start))/float64(time.Millisecond), after.Size())
}
