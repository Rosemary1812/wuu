package session

import (
	"testing"
	"time"
)

// A corrupt journal row must not wedge recovery for every other thread: the
// scan quarantines it (payload and reason preserved) and keeps returning the
// healthy records.
func TestPendingResidentAdmissionCompensationsQuarantinesCorruptRows(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateWithMetadata(dir, "quarantine-dm", "/tmp/project"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	valid := ResidentAdmissionCompensation{
		ID: "quarantine-valid", ParticipantID: "quarantine-worker", ThreadID: "quarantine-dm",
		CreatedAt: now,
	}
	if err := SaveResidentAdmissionCompensation(dir, valid); err != nil {
		t.Fatal(err)
	}

	db, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO resident_admission_compensations (id, thread_id, payload_json, created_at)
VALUES ('quarantine-bad', 'quarantine-dm', '{not-json', ?)`, now.Add(-time.Second).UnixNano()); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	pending, err := PendingResidentAdmissionCompensations(dir)
	if err != nil {
		t.Fatalf("scan with corrupt row must not fail: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != "quarantine-valid" {
		t.Fatalf("pending = %+v, want only the valid record", pending)
	}

	db, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var reason string
	if err := db.QueryRow(`SELECT reason FROM resident_admission_compensations_quarantine WHERE id = 'quarantine-bad'`).Scan(&reason); err != nil {
		t.Fatalf("corrupt row was not quarantined: %v", err)
	}
	if reason == "" {
		t.Fatal("quarantine reason must be recorded for manual repair")
	}
	var remaining int
	if err := db.QueryRow(`SELECT COUNT(*) FROM resident_admission_compensations WHERE id = 'quarantine-bad'`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatal("corrupt row must leave the live journal after quarantine")
	}
}
