// task-rules-audit reads an explicitly supplied offline SQLite database only.
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	_ "github.com/mattn/go-sqlite3"
	"io"
	"limmr/internal/tasks"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(args []string, output io.Writer) error {
	flags := flag.NewFlagSet("task-rules-audit", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	protocol := flags.Bool("protocol", false, "Plan the one-time protocol pass, without applying it")
	keep := flags.String("keep", "", "Comma-separated task IDs kept in the owner review (requires --protocol)")
	format := flags.String("format", "", "Output format: text or json (protocol defaults to text)")
	path := flags.String("database", "", "Offline hub database (required)")
	at := flags.String("now", "", "RFC3339 audit time (default now)")
	zone := flags.String("timezone", time.Local.String(), "Local IANA time zone")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	if *format == "" {
		*format = "json"
		if *protocol {
			*format = "text"
		}
	}
	if *format != "json" && *format != "text" {
		return fmt.Errorf("--format must be text or json")
	}
	if !*protocol && *format == "text" {
		return fmt.Errorf("--format text requires --protocol")
	}
	if *keep != "" && !*protocol {
		return fmt.Errorf("--keep requires --protocol")
	}
	keepIDs := []string{}
	if *keep != "" {
		for _, id := range strings.Split(*keep, ",") {
			id = strings.TrimSpace(id)
			if id == "" {
				return fmt.Errorf("--keep requires nonempty task IDs")
			}
			keepIDs = append(keepIDs, id)
		}
	}
	if *path == "" {
		return fmt.Errorf("--database is required; use a fixture or offline copy")
	}
	now := time.Now()
	var err error
	if *at != "" {
		now, err = time.Parse(time.RFC3339, *at)
		if err != nil {
			return fmt.Errorf("invalid --now")
		}
	}
	loc, err := time.LoadLocation(*zone)
	if err != nil {
		return fmt.Errorf("invalid --timezone")
	}
	db, err := openOffline(*path)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var out any
	if *protocol {
		plan, err := tasks.PlanProtocolPass(db, now, loc, tasks.ProtocolOptions{KeepTaskIDs: keepIDs})
		if err != nil {
			return err
		}
		if *format == "text" {
			return tasks.WriteProtocolText(output, plan)
		}
		out = plan
	} else {
		out, err = tasks.AuditDatabase(db, now, loc)
	}
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(out)
}

func forbiddenPath(path string) bool {
	path = strings.ToLower(filepath.Clean(path)) + "/"
	return strings.Contains(path, "/library/application support/limmr/") || strings.Contains(path, "/whatsapp-mcp/")
}

func openOffline(path string) (*sql.DB, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	// Refuse lexical live paths before even resolving or statting them.
	if forbiddenPath(abs) {
		return nil, fmt.Errorf("live data paths are forbidden; supply an offline copy")
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	if forbiddenPath(abs) {
		return nil, fmt.Errorf("live data paths are forbidden; supply an offline copy")
	}
	u := url.URL{Scheme: "file", Path: abs}
	u.RawQuery = url.Values{"mode": {"ro"}, "immutable": {"1"}}.Encode()
	db, err := sql.Open("sqlite3", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}
