//go:build integration

package app

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBuildQueueResumesAndContinuesAfterFailure(t *testing.T) {
	history, err := OpenHistory(freshDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	defer history.Close()
	firstRequest := BuildRequest{Mode: BuildNote, NoteID: "math/test/target"}
	id, err := history.enqueueBuild(t.Context(), firstRequest)
	if err != nil {
		t.Fatal(err)
	}
	if err := history.updateQueuedBuild(t.Context(), BuildStatus{BuildTask: BuildTask{ID: id}, State: "running", StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 2)
	proceed := make(chan struct{})
	finished := make(chan BuildStatus, 2)
	builder := NewBuildManager(t.TempDir(), 10*time.Second, "/", "/api/", history,
		func(ctx context.Context) ([]Note, []string, error) {
			started <- struct{}{}
			select {
			case <-proceed:
				return nil, nil, errors.New("render input unavailable")
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			}
		}, nil)
	builder.SetOnFinish(func(status BuildStatus) { finished <- status })
	defer close(proceed)
	if err := builder.Resume(t.Context()); err != nil {
		t.Fatal(err)
	}
	waitForBuildEvent(t, started)
	request := BuildRequest{Mode: BuildAll}
	status, err := builder.Start(t.Context(), request)
	if err != nil || status.ID == id || status.State != "queued" || status.Mode != BuildAll {
		t.Fatalf("enqueue while active: %+v, %v", status, err)
	}
	duplicate, err := builder.Start(t.Context(), request)
	if err != nil || duplicate.ID != status.ID || builder.Status().Queued != 1 {
		t.Fatalf("duplicate queue request: %+v, %v", duplicate, err)
	}
	select {
	case <-started:
		t.Fatal("queued build started before the first finished")
	default:
	}
	pending, err := history.resumeBuildQueue(t.Context())
	if err != nil || len(pending) != 2 || pending[0].ID != id || pending[0].BuildRequest != firstRequest || pending[1].BuildRequest != request {
		t.Fatalf("persisted queue: %v, %v", pending, err)
	}
	proceed <- struct{}{}
	first := waitForBuildEvent(t, finished)
	waitForBuildEvent(t, started)
	status = builder.Status()
	if status.ID != pending[1].ID || status.Queued != 0 {
		t.Fatalf("queue did not advance: %+v", status)
	}
	proceed <- struct{}{}
	second := waitForBuildEvent(t, finished)
	if first.ID != id || first.BuildRequest != firstRequest || second.ID != pending[1].ID || first.State != "failed" || second.State != "failed" {
		t.Fatalf("queue completion: %+v, %+v", first, second)
	}
}

func waitForBuildEvent[T any](t *testing.T, events <-chan T) T {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the build queue")
		var zero T
		return zero
	}
}
