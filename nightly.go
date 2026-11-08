package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Damba017/leitner/internal/store"
)

const (
	nightlyKeep   = 7                 // nightly backups kept
	nightlyHour   = 3                 // Amsterdam time; outside the 02:00-03:00 DST change
	nightlyMinute = 30                //
	nightlyRetry  = time.Hour         // after a failed backup
	nightlyLayout = "20060102-150405" // in leitner-<layout>.db, Amsterdam time
)

var amsterdam = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Amsterdam") // main.go imports time/tzdata
	if err != nil {
		panic(err)
	}
	return loc
}()

// nightlyBackups makes one backup a day in <db folder>/backups/nightly and keeps
// the newest nightlyKeep. update.sh keeps its own in <db folder>/backups
// (leitner-*.db there), so neither prunes the other's files.
type nightlyBackups struct {
	dbPath string
	dir    string // <db folder>/backups/nightly
}

func newNightlyBackups(dbPath string) *nightlyBackups {
	return &nightlyBackups{dbPath: dbPath, dir: filepath.Join(filepath.Dir(dbPath), "backups", "nightly")}
}

// lastNightly is the most recent 03:30 (Amsterdam) at or before now.
func lastNightly(now time.Time) time.Time {
	y, m, d := now.In(amsterdam).Date()
	t := time.Date(y, m, d, nightlyHour, nightlyMinute, 0, 0, amsterdam)
	if t.After(now) {
		t = time.Date(y, m, d-1, nightlyHour, nightlyMinute, 0, 0, amsterdam)
	}
	return t
}

// nextNightly is the first 03:30 (Amsterdam) after now.
func nextNightly(now time.Time) time.Time {
	y, m, d := lastNightly(now).Date()
	return time.Date(y, m, d+1, nightlyHour, nightlyMinute, 0, 0, amsterdam)
}

func backupName(t time.Time) string {
	return "leitner-" + t.In(amsterdam).Format(nightlyLayout) + ".db"
}

// backupTime parses a nightly backup's file name; ok is false for any other file.
func backupTime(name string) (time.Time, bool) {
	stamp, ok := strings.CutPrefix(name, "leitner-")
	if !ok {
		return time.Time{}, false
	}
	stamp, ok = strings.CutSuffix(stamp, ".db")
	if !ok {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation(nightlyLayout, stamp, amsterdam)
	return t, err == nil && backupName(t) == name
}

// files lists the nightly backups, oldest first. Other files are ignored.
func (n *nightlyBackups) files() []string {
	entries, _ := os.ReadDir(n.dir) // a missing folder just means no backups yet
	var names []string
	for _, e := range entries {
		if _, ok := backupTime(e.Name()); ok && e.Type().IsRegular() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // the stamp sorts by time
	return names
}

func (n *nightlyBackups) latest() (time.Time, bool) {
	names := n.files()
	if len(names) == 0 {
		return time.Time{}, false
	}
	return backupTime(names[len(names)-1])
}

// backup writes the backup for now, then prunes.
func (n *nightlyBackups) backup(ctx context.Context, now time.Time) error {
	if err := os.MkdirAll(n.dir, 0o700); err != nil {
		return fmt.Errorf("nightly backup: %w", err)
	}
	if err := store.Backup(ctx, n.dbPath, filepath.Join(n.dir, backupName(now))); err != nil {
		return err
	}
	n.prune()
	return nil
}

func (n *nightlyBackups) prune() {
	names := n.files()
	if len(names) <= nightlyKeep {
		return
	}
	for _, name := range names[:len(names)-nightlyKeep] {
		if err := os.Remove(filepath.Join(n.dir, name)); err != nil {
			slog.Warn("remove old nightly backup", "file", name, "err", err)
		}
	}
}

// run makes the 03:30 backup every night. At start (and after a failure) it
// catches up when the newest backup is older than the last 03:30, e.g. because
// the laptop was off.
func (n *nightlyBackups) run(ctx context.Context) {
	for {
		now := time.Now()
		wait := time.Until(nextNightly(now))
		if t, ok := n.latest(); !ok || t.Before(lastNightly(now)) {
			if err := n.backup(ctx, now); err != nil {
				if ctx.Err() != nil {
					return
				}
				slog.Error("nightly backup failed", "err", err)
				wait = min(wait, nightlyRetry)
			} else {
				slog.Info("nightly backup made", "file", backupName(now))
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}
