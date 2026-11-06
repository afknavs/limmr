package sync_test

import (
	"testing"
	"time"
)

func TestOct29OwnerNoteConcurrentAddEditAndDeleteSync(t *testing.T) {
	a, b := newEngine(t), newEngine(t)
	task(t, a, "study", "Study", nil)
	pair(t, a, b)
	exchange(t, a, b)
	av, err := a.tasks.OwnerNote(nil, "study", "", "Venue is North Hall", false)
	must(t, err)
	bv, err := b.tasks.OwnerNote(nil, "study", "", "Involve Maya", false)
	must(t, err)
	exchange(t, a, b)
	for _, e := range []*engine{a, b} {
		v, err := e.tasks.Get(nil, "study")
		must(t, err)
		if len(v.OwnerNotes) != 2 {
			t.Fatal("concurrent notes lost")
		}
	}
	_, err = a.tasks.OwnerNote(nil, "study", av.OwnerNotes[0].ID, "Venue is South Hall", false)
	must(t, err)
	_, err = b.tasks.OwnerNote(nil, "study", bv.OwnerNotes[0].ID, "", true)
	must(t, err)
	exchange(t, a, b)
	exchange(t, a, b)
	for _, e := range []*engine{a, b} {
		v, err := e.tasks.Get(nil, "study")
		must(t, err)
		if len(v.OwnerNotes) != 1 || v.OwnerNotes[0].Text != "Venue is South Hall" {
			t.Fatal("note edit/delete lost")
		}
	}
}
func TestOct29DueEventCreatesPeerNotificationAndReceiptOnce(t *testing.T) {
	a, b := newEngine(t), newEngine(t)
	task(t, a, "study", "Study", nil)
	pair(t, a, b)
	_, err := a.h.Repo.DB.Exec(`INSERT INTO task_events VALUES('study:due','study',?,'task_due','Check-in fired','{}')`, a.now.UTC().Format(time.RFC3339Nano))
	must(t, err)
	exchange(t, a, b)
	must(t, b.tasks.ReceiveCheckIns())
	must(t, b.tasks.ReceiveCheckIns())
	var n int
	must(t, b.h.Repo.DB.QueryRow(`SELECT count(*) FROM objects WHERE bucket='suggestions' AND json_extract(data,'$.task_id')='study' AND json_extract(data,'$.kind')='check_in'`).Scan(&n))
	if n != 1 {
		t.Fatal("peer notification missing or duplicate", n)
	}
	exchange(t, a, b)
	must(t, a.h.Repo.DB.QueryRow(`SELECT count(*) FROM task_events WHERE kind='check_in_received' AND task_id='study'`).Scan(&n))
	if n != 1 {
		t.Fatal("delivery receipt missing")
	}
}
