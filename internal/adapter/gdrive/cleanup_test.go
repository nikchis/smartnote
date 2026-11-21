package gdrive

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
)

// deletedFiles returns the IDs of the files DELETE asked for.
func deletedFiles(d *fakeDrive) []string {
	var ids []string

	for _, r := range d.reqs() {
		if id, ok := strings.CutPrefix(r.path, filesPath+"/"); ok && r.method == http.MethodDelete {
			ids = append(ids, id)
		}
	}

	return ids
}

// TestCleanupRemovesSurvivingDuplicate: duplicates that a crashed creator
// left are deleted by the next successful write of another device, at most
// 10 per write (spec §5.2).
func TestCleanupRemovesSurvivingDuplicate(t *testing.T) {
	d := newFakeDrive(t)
	effective := d.put(container, []byte("v1"), testNow)

	for i := range 12 {
		d.put(container, fmt.Appendf(nil, "dup-%d", i), testNow.Add(time.Duration(i+1)*time.Minute))
	}

	s := uploadStore(t, d, &memUploads{})
	_, v1, _ := mustRead(t, s)
	mustWrite(t, s, []byte("v2"), v1)

	if n := len(deletedFiles(d)); n != 10 {
		t.Errorf("deleted after the first write = %d, want 10", n)
	}

	if n := len(d.containerData()); n != 3 {
		t.Errorf("files after the first write = %d, want 3", n)
	}

	other := uploadStore(t, d, &memUploads{})
	_, v2, _ := mustRead(t, other)
	mustWrite(t, other, []byte("v3"), v2)

	if got := d.containerData(); len(got) != 1 || got[0] != "v3" {
		t.Errorf("container files = %q, want only the effective one", got)
	}

	for _, id := range deletedFiles(d) {
		if id == effective {
			t.Error("the effective file was deleted")
		}
	}
}

// TestCleanupFailureIsOnlyLogged: a failed deletion of a duplicate does not
// fail the write; it is logged without IDs.
func TestCleanupFailureIsOnlyLogged(t *testing.T) {
	d := newFakeDrive(t)
	id := d.put(container, []byte("v1"), testNow)
	dup := d.put(container, []byte("dup"), testNow.Add(time.Minute))
	d.set(func(d *fakeDrive) {
		d.fail[epDelete] = fakeFailure{status: http.StatusInternalServerError, reason: "backendError"}
	})

	var log logBuffer

	opts := testOptions(d)
	opts.log = slog.New(slog.NewTextHandler(&log, nil))
	s := newStore(d.endpoints(), testTokens(), fakeClient, testRefresher(t, d), &memUploads{}, opts)

	mustWrite(t, s, []byte("v2"), id+":"+md5Of([]byte("v1")))

	got := log.String()
	if !strings.Contains(got, "duplicate") || strings.Contains(got, dup) || strings.Contains(got, fakeMessage) {
		t.Errorf("log = %q, want the failed deletion without the ID and the message", got)
	}
}

// TestCleanupNeverDeletesEarliest: the cleanup deletes only files that have
// an earlier one in the list just read, so the earliest file of all pages
// (on equal createdTime the smaller ID) is never deleted.
func TestCleanupNeverDeletesEarliest(t *testing.T) {
	d := newFakeDrive(t)
	d.set(func(d *fakeDrive) {
		d.pageLimit = 2
		d.ignoreOrder = true // the list keeps the creation order
	})

	d.putID("file-d", container, []byte("dup-1"), testNow.Add(time.Minute))
	d.putID("file-e", container, []byte("dup-2"), testNow.Add(2*time.Minute))
	d.putID("file-f", container, []byte("dup-3"), testNow.Add(3*time.Minute))
	d.putID("file-c", container, []byte("same time, larger id"), testNow)
	d.putID("file-b", container, []byte("v1"), testNow) // the earliest, on the last page

	s := uploadStore(t, d, &memUploads{})

	data, version, _ := mustRead(t, s)
	if string(data) != "v1" {
		t.Fatalf("Read = %q, want the earliest file", data)
	}

	mustWrite(t, s, []byte("v2"), version)

	deleted := deletedFiles(d)
	if len(deleted) != 4 {
		t.Errorf("deleted = %v, want the 4 later files", deleted)
	}

	for _, id := range deleted {
		if id == "file-b" {
			t.Error("the earliest file was deleted")
		}
	}

	if got := d.containerData(); len(got) != 1 || got[0] != "v2" {
		t.Errorf("container files = %q, want only the earliest with the replacement", got)
	}
}

// TestCleanupIgnoresRepeatedID: a file that two pages of the list both give
// is one candidate, so the cleanup after a replacement does not delete the
// effective file (spec §5.2).
func TestCleanupIgnoresRepeatedID(t *testing.T) {
	d := newFakeDrive(t)
	d.set(func(d *fakeDrive) {
		d.pageLimit = 1
		d.listTwice = true
	})

	effective := d.put(container, []byte("v1"), testNow)
	s := uploadStore(t, d, &memUploads{})

	_, version, _ := mustRead(t, s)
	mustWrite(t, s, []byte("v2"), version)

	if deleted := deletedFiles(d); len(deleted) != 0 {
		t.Errorf("deleted = %v, want none (the effective file is %s)", deleted, effective)
	}

	if got := d.containerData(); len(got) != 1 || got[0] != "v2" {
		t.Errorf("container files = %q, want only the effective one with the replacement", got)
	}
}

// TestCreateIgnoresRepeatedID: the own created file that two pages of the
// list both give is the effective one and is not deleted as its own
// duplicate.
func TestCreateIgnoresRepeatedID(t *testing.T) {
	d := newFakeDrive(t)
	d.set(func(d *fakeDrive) {
		d.pageLimit = 1
		d.listTwice = true
	})

	s := uploadStore(t, d, &memUploads{})
	mustWrite(t, s, []byte("v1"), "")

	if deleted := deletedFiles(d); len(deleted) != 0 {
		t.Errorf("deleted = %v, want none", deleted)
	}

	if got := d.containerData(); len(got) != 1 || got[0] != "v1" {
		t.Errorf("container files = %q, want the created one", got)
	}
}

// TestRemoveDuplicatesSkipsEffectiveID: the cleanup never deletes the ID of
// the effective file, even when the list it gets repeats it.
func TestRemoveDuplicatesSkipsEffectiveID(t *testing.T) {
	d := newFakeDrive(t)
	d.putID("file-a", container, []byte("v1"), testNow)
	d.putID("file-b", container, []byte("dup"), testNow.Add(time.Minute))

	s := uploadStore(t, d, &memUploads{})
	s.removeDuplicates(t.Context(), []driveFile{{ID: "file-a"}, {ID: "file-a"}, {ID: "file-b"}})

	if got := deletedFiles(d); len(got) != 1 || got[0] != "file-b" {
		t.Errorf("deleted = %v, want only file-b", got)
	}
}
