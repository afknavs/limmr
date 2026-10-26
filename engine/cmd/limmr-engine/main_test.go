package main

import (
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestInvalidListenerFlagsFailBeforeCreatingStores(t *testing.T) {
	for _, ports := range [][2]int{{0, 18801}, {18800, 0}, {-1, 18801}, {65536, 18801}, {18800, 18800}} {
		t.Run(strconv.Itoa(ports[0])+"_"+strconv.Itoa(ports[1]), func(t *testing.T) {
			home := filepath.Join(t.TempDir(), "unused")
			t.Setenv("LIMMR_HOME", home)
			oldFlags, oldArgs := flag.CommandLine, os.Args
			defer func() { flag.CommandLine = oldFlags; os.Args = oldArgs }()
			flag.CommandLine = flag.NewFlagSet("fixture", flag.ContinueOnError)
			os.Args = []string{"limmr-engine", "--demo", "--port", strconv.Itoa(ports[0]), "--remote-port", strconv.Itoa(ports[1])}
			if run() == nil {
				t.Fatal("invalid listener ports accepted")
			}
			if _, err := os.Stat(home); !os.IsNotExist(err) {
				t.Fatal("invalid arguments created stores")
			}
		})
	}
}

func TestPlanRefreshDoesNotCreateWhatsAppSessionOrListener(t *testing.T) {
	home := t.TempDir()
	t.Setenv("LIMMR_HOME", home)
	oldFlags, oldArgs := flag.CommandLine, os.Args
	defer func() { flag.CommandLine = oldFlags; os.Args = oldArgs }()
	flag.CommandLine = flag.NewFlagSet("fixture", flag.ContinueOnError)
	os.Args = []string{"limmr-engine", "plan-refresh", "--demo"}
	// No socket can be opened in this sandbox, so a successful run also verifies
	// the CLI returns before listener/live-client construction.
	if e := run(); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(home, "session.db")); !os.IsNotExist(e) {
		t.Fatal("plan command opened phone-session storage")
	}
}
