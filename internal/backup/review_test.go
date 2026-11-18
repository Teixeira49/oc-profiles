package backup

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Teixeira49/oc-profiles/internal/core"
)

func TestRecoverFallsBackWhenNewestPayloadIsInvalid(t *testing.T) {
	p := setup(t)
	addProfile(t, p, "alpha")
	mustSnapshot(t, p) // #1 valid
	// #2: valid checksum, but the payload is not a valid store (duplicate names).
	bad := []byte(`{"version": 2, "profiles": [{"name": "dup", "models": {}}, {"name": "dup", "models": {}}]}`)
	_, templates, _ := core.ReadStoreFiles(p)
	if _, _, err := Write(p.BackupDir, bad, templates); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(p.StoreDir); err != nil {
		t.Fatal(err)
	}
	rep, err := Recover(p)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Restored || rep.From.Seq != 1 || !strings.Contains(strings.Join(rep.Warnings, "\n"), "skipped snapshot #2") {
		t.Fatalf("report = %+v", rep)
	}
	// RestoreSeq without --seq falls back the same way; an explicit bad seq errors.
	if info, _, err := RestoreSeq(p, 0); err != nil || info.Seq != 1 {
		t.Fatalf("restore newest: %+v %v", info, err)
	}
	if _, _, err := RestoreSeq(p, 2); err == nil || !strings.Contains(err.Error(), "duplicate profile name") {
		t.Fatalf("restore #2: %v", err)
	}
}

func TestRecoverRefusesNewerStoreWithoutMovingIt(t *testing.T) {
	p := setup(t)
	mustSnapshot(t, p)
	newer := []byte(`{"version": 99, "profiles": []}`)
	if err := os.WriteFile(p.ProfilesFile(), newer, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Recover(p); err == nil || !strings.Contains(err.Error(), "store was written by a newer oc-profiles") {
		t.Fatalf("err = %v", err)
	}
	if got, _ := os.ReadFile(p.ProfilesFile()); !bytes.Equal(got, newer) {
		t.Fatal("newer store modified")
	}
	if m, _ := filepath.Glob(p.ProfilesFile() + ".*"); len(m) != 0 {
		t.Fatalf("files moved aside: %v", m)
	}
	if pr := Diagnose(p); pr.NeedsRecovery() {
		t.Fatal("a newer store must not be diagnosed as corrupt")
	}
}

func TestNewestValidPicksHighestSequence(t *testing.T) {
	p := setup(t)
	for _, n := range []string{"a", "b", "c"} {
		addProfile(t, p, n)
		mustSnapshot(t, p)
	}
	// current.json is stale (a copy of #2) while history holds #3.
	h2, _ := os.ReadFile(filepath.Join(p.BackupDir, "history", "snap-000002.json"))
	if err := os.WriteFile(filepath.Join(p.BackupDir, "current.json"), h2, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, info, _, ok := NewestValid(p.BackupDir); !ok || info.Seq != 3 {
		t.Fatalf("newest = %+v", info)
	}
}

func TestAsideNamesAreUniqueAndSwapIsAllOrNothing(t *testing.T) {
	p := setup(t)
	addProfile(t, p, "alpha")
	mustSnapshot(t, p)
	// Two recoveries at the same (frozen) instant must not collide.
	frozen := Now()
	core.Now = func() time.Time { return frozen }
	t.Cleanup(func() { core.Now = time.Now })
	for i := 0; i < 2; i++ {
		if err := os.WriteFile(p.ProfilesFile(), []byte("garbage"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Recover(p); err != nil {
			t.Fatal(err)
		}
	}
	m, _ := filepath.Glob(p.ProfilesFile() + ".corrupt-*")
	if len(m) != 2 {
		t.Fatalf("aside files = %v", m)
	}

	// A failing swap (read-only store dir) leaves the store untouched.
	before, _ := os.ReadFile(p.ProfilesFile())
	if err := os.Chmod(p.StoreDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(p.StoreDir, 0o755) })
	s, _ := core.LoadStore(p)
	if _, err := swapInStore(p, s, "replaced"); err == nil {
		t.Fatal("expected swap to fail on a read-only store dir")
	}
	os.Chmod(p.StoreDir, 0o755)
	if got, _ := os.ReadFile(p.ProfilesFile()); !bytes.Equal(got, before) {
		t.Fatal("failed swap changed the store")
	}
	if left, _ := filepath.Glob(filepath.Join(p.StoreDir, "*.staged-*")); len(left) != 0 {
		t.Fatalf("staged files left: %v", left)
	}
}
