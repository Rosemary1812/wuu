package session

import (
	"reflect"
	"testing"
)

func TestHeldUserWorkRoundTripDeleteAndCascade(t *testing.T) {
	dir := t.TempDir()
	sess, err := Create(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []HeldUserWork{
		{ID: "steer-1", Origin: HeldUserWorkOriginSteer, MessageJSON: []byte(`{"Content":"guide"}`), RuntimeJSON: []byte(`{"Model":"a"}`)},
		{ID: "queue-1", Origin: HeldUserWorkOriginQueue, MessageJSON: []byte(`{"Content":"first"}`), RuntimeJSON: []byte(`{"Model":"b"}`)},
		{ID: "queue-2", Origin: HeldUserWorkOriginQueue, MessageJSON: []byte(`{"Content":"second"}`), RuntimeJSON: []byte(`{"Model":"c"}`)},
	}
	if err := ReplaceHeldUserWork(dir, sess.ID, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadHeldUserWork(dir, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	for index := range want {
		want[index].SessionID = sess.ID
		want[index].Position = index
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadHeldUserWork() = %+v, want %+v", got, want)
	}

	removed, err := DeleteHeldUserWork(dir, sess.ID, "queue-1")
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("DeleteHeldUserWork returned false")
	}
	got, err = LoadHeldUserWork(dir, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "steer-1" || got[0].Position != 0 || got[1].ID != "queue-2" || got[1].Position != 1 {
		t.Fatalf("delete did not preserve compact order: %+v", got)
	}

	if _, err := Delete(dir, sess.ID); err != nil {
		t.Fatal(err)
	}
	got, err = LoadHeldUserWork(dir, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("session delete did not cascade held work: %+v", got)
	}
}

func TestReplaceHeldUserWorkRollsBackInvalidReplacement(t *testing.T) {
	dir := t.TempDir()
	sess, err := Create(dir)
	if err != nil {
		t.Fatal(err)
	}
	original := []HeldUserWork{{
		ID: "queue-1", Origin: HeldUserWorkOriginQueue,
		MessageJSON: []byte(`{"Content":"keep"}`), RuntimeJSON: []byte(`{}`),
	}}
	if err := ReplaceHeldUserWork(dir, sess.ID, original); err != nil {
		t.Fatal(err)
	}
	invalid := append(original, HeldUserWork{ID: "bad", Origin: "unknown", MessageJSON: []byte(`{}`), RuntimeJSON: []byte(`{}`)})
	if err := ReplaceHeldUserWork(dir, sess.ID, invalid); err == nil {
		t.Fatal("ReplaceHeldUserWork accepted invalid origin")
	}
	got, err := LoadHeldUserWork(dir, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "queue-1" {
		t.Fatalf("failed replacement was not rolled back: %+v", got)
	}
}
