package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chopsticks-user/gsm/internal/proc"
)

func write(t *testing.T, path, data string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func countObjects(t *testing.T, s Store) int {
	n := 0
	filepath.WalkDir(filepath.Join(s.Dir, "objects"), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return nil
	})
	return n
}

func setup(t *testing.T) (Store, string) {
	d := t.TempDir()
	saves := filepath.Join(d, "saves")
	write(t, filepath.Join(saves, "Level.sav"), "level v1")
	write(t, filepath.Join(saves, "Players", "p1.sav"), "player")
	return Store{Dir: filepath.Join(d, "store")}, saves
}

func TestSnapshotDedupAndRestore(t *testing.T) {
	s, saves := setup(t)
	meta := Record{Instance: "main", Game: "palworld", Trigger: "manual"}
	r1, err := s.Snapshot(saves, meta)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Snapshot(saves, meta); err != nil {
		t.Fatal(err)
	}
	if n := countObjects(t, s); n != 2 {
		t.Fatalf("two identical snapshots stored %d objects, want 2", n)
	}

	write(t, filepath.Join(saves, "Level.sav"), "level v2")
	write(t, filepath.Join(saves, "extra.sav"), "new")
	safe, err := s.Restore(r1, saves, Record{Instance: "main", Trigger: "restore"})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(saves, "Level.sav")); string(b) != "level v1" {
		t.Fatalf("Level.sav = %q", b)
	}
	if _, err := os.Stat(filepath.Join(saves, "extra.sav")); err == nil {
		t.Fatal("file absent from the snapshot survived the restore")
	}
	if len(safe.Files) != 3 {
		t.Fatalf("safety snapshot has %d files, want 3", len(safe.Files))
	}
	all, _ := s.List("main")
	if len(all) != 3 {
		t.Fatalf("List = %d records, want 3", len(all))
	}
	if got, err := s.Get("main", r1.ID[:4]); err != nil || got.ID != r1.ID {
		t.Fatalf("Get by prefix: %v, %v", got.ID, err)
	}
}

func TestCorruptObjectLeavesLiveSavesUntouched(t *testing.T) {
	s, saves := setup(t)
	r1, err := s.Snapshot(saves, Record{Instance: "main"})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range r1.Files {
		if f.Path == "Level.sav" {
			if err := os.WriteFile(s.objectPath(f.Hash), []byte("garbage"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	write(t, filepath.Join(saves, "Level.sav"), "live")
	if _, err := s.Restore(r1, saves, Record{Instance: "main"}); err == nil {
		t.Fatal("want error")
	}
	if b, _ := os.ReadFile(filepath.Join(saves, "Level.sav")); string(b) != "live" {
		t.Fatalf("live save changed to %q", b)
	}
	if _, err := os.Stat(saves + ".restore"); err == nil {
		t.Fatal("staging directory left behind")
	}
}

func TestRecordlessSnapshotDoesNotExist(t *testing.T) {
	s, saves := setup(t)
	if _, err := s.Snapshot(saves, Record{Instance: "main"}); err != nil {
		t.Fatal(err)
	}
	// Simulate a crash before the record write: objects exist, the record doesn't.
	recs, _ := filepath.Glob(filepath.Join(s.recordDir("main"), "*.json"))
	for _, r := range recs {
		os.Remove(r)
	}
	if all, err := s.List("main"); err != nil || len(all) != 0 {
		t.Fatalf("List = %v, %v", all, err)
	}
}

func TestInterruptedSwapIsRepaired(t *testing.T) {
	s, saves := setup(t)
	r1, _ := s.Snapshot(saves, Record{Instance: "main"})
	// Crash between the two renames: saves moved to saves.old, new dir not in place.
	if err := os.Rename(saves, saves+".old"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Restore(r1, saves, Record{Instance: "main"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(saves, "Level.sav")); string(b) != "level v1" {
		t.Fatalf("Level.sav = %q", b)
	}
}

func TestStaleLockIsTakenOver(t *testing.T) {
	s, _ := setup(t)
	os.MkdirAll(s.Dir, 0o755)
	b, _ := json.Marshal(proc.State{PID: os.Getpid(), Start: "not-my-start-time"})
	write(t, filepath.Join(s.Dir, "lock"), string(b))
	unlock, err := s.Lock(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	// A live holder blocks a second taker.
	if _, err := s.Lock(300 * time.Millisecond); err == nil {
		t.Fatal("second lock succeeded while the first is held")
	}
	unlock()
	unlock2, err := s.Lock(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	unlock2()
}

// gsm publishes its lock complete, so an empty or partial lock can only be
// left by a crash or an older gsm, and is taken over at once.
func TestUnreadableLockIsTakenOver(t *testing.T) {
	for name, content := range map[string]string{"empty": "", "garbage": `{"pid":12`} {
		t.Run(name, func(t *testing.T) {
			s, _ := setup(t)
			if err := os.MkdirAll(s.Dir, 0o755); err != nil {
				t.Fatal(err)
			}
			write(t, filepath.Join(s.Dir, "lock"), content)
			unlock, err := s.Lock(300 * time.Millisecond)
			if err != nil {
				t.Fatal(err)
			}
			unlock()
		})
	}
}

// The lock is never visible empty, and taking and releasing it leaves only
// the store's own folders behind.
func TestLockIsPublishedCompleteAndCleansUp(t *testing.T) {
	s, _ := setup(t)
	unlock, err := s.Lock(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var owner proc.State
	b, err := os.ReadFile(filepath.Join(s.Dir, "lock"))
	if err != nil || json.Unmarshal(b, &owner) != nil || owner.PID != os.Getpid() {
		t.Fatalf("lock content %q (read error %v) doesn't name this process", b, err)
	}
	unlock()
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "lock") {
			t.Errorf("%s left in the store after unlock", e.Name())
		}
	}
}
