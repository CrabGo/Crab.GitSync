package gitengine

import (
	"context"
	"testing"
)

func TestInspectCommitRelationships(t *testing.T) {
	path := t.TempDir()
	initRepo(t, path)
	check := func(want string, ahead, behind int) {
		t.Helper()
		r, err := Inspect(context.Background(), path)
		if err != nil || r.SyncStatus != want || r.Ahead != ahead || r.Behind != behind || r.LastSuccessfulFetch != "" {
			t.Fatalf("want %s %d/%d, got %+v err=%v", want, ahead, behind, r, err)
		}
	}
	check("no-upstream", 0, 0) // Empty repositories remain valid, without a freshness claim.
	commit(t, path, "file.txt", "initial")
	git(t, path, "branch", "baseline")
	git(t, path, "branch", "--set-upstream-to=baseline", "main")
	check("synced", 0, 0)
	commit(t, path, "file.txt", "main change")
	check("ahead", 1, 0)
	git(t, path, "checkout", "baseline")
	git(t, path, "branch", "--set-upstream-to=main", "baseline")
	check("behind", 0, 1)
	commit(t, path, "other.txt", "baseline change")
	check("diverged", 1, 1)
	git(t, path, "branch", "-D", "main")
	check("unknown", 0, 0) // An upstream configuration with a missing ref is not synchronized.
	git(t, path, "checkout", "--detach")
	check("unknown", 0, 0)
}
