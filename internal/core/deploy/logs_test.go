package deploy_test

import (
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/core/deploy"
)

// collect drains a follow channel until it closes or the deadline passes.
func collect(t *testing.T, ch <-chan string, want int) []string {
	t.Helper()
	var got []string
	deadline := time.After(2 * time.Second)
	for len(got) < want {
		select {
		case line, open := <-ch:
			if !open {
				return got
			}
			got = append(got, line)
		case <-deadline:
			t.Fatalf("waited for %d lines, got %d: %v", want, len(got), got)
		}
	}
	return got
}

func TestOutputIsSplitIntoLines(t *testing.T) {
	store := deploy.NewLogStore()
	w := store.Writer("dep_01HQ8")

	n, err := io.WriteString(w, "step 1\nstep 2\n")
	require.NoError(t, err)
	require.Equal(t, len("step 1\nstep 2\n"), n, "a Writer reports every byte it took")

	backlog, _, cancel := store.Follow("dep_01HQ8")
	defer cancel()
	require.Equal(t, []string{"step 1", "step 2"}, backlog)
}

// A build writes in whatever chunks the runtime hands over, and a line can
// arrive split across several of them.
func TestALineSplitAcrossWritesIsReassembled(t *testing.T) {
	store := deploy.NewLogStore()
	w := store.Writer("dep_01HQ8")

	for _, chunk := range []string{"Step 1/4 ", ": FROM ", "golang:1.27\nStep 2/4"} {
		_, err := io.WriteString(w, chunk)
		require.NoError(t, err)
	}

	backlog, _, cancel := store.Follow("dep_01HQ8")
	cancel()
	require.Equal(t, []string{"Step 1/4 : FROM golang:1.27"}, backlog,
		"the unterminated line is not emitted yet")

	// Close flushes the partial line rather than losing it: the last line of a
	// build is often the one that says why it failed.
	require.NoError(t, w.Close())
	backlog, _, cancel = store.Follow("dep_01HQ8")
	cancel()
	require.Equal(t, []string{"Step 1/4 : FROM golang:1.27", "Step 2/4"}, backlog)
}

// The backlog is returned with the channel rather than replayed through it, so
// a console that connects late still sees the whole build.
func TestFollowReturnsTheBacklogAndThenNewLines(t *testing.T) {
	store := deploy.NewLogStore()
	w := store.Writer("dep_01HQ8")
	_, err := io.WriteString(w, "already happened\n")
	require.NoError(t, err)

	backlog, ch, cancel := store.Follow("dep_01HQ8")
	defer cancel()
	require.Equal(t, []string{"already happened"}, backlog)

	_, err = io.WriteString(w, "happening now\n")
	require.NoError(t, err)
	require.Equal(t, []string{"happening now"}, collect(t, ch, 1))
}

func TestEveryFollowerSeesEveryLine(t *testing.T) {
	store := deploy.NewLogStore()
	w := store.Writer("dep_01HQ8")

	_, first, cancelFirst := store.Follow("dep_01HQ8")
	defer cancelFirst()
	_, second, cancelSecond := store.Follow("dep_01HQ8")
	defer cancelSecond()

	_, err := io.WriteString(w, "building\n")
	require.NoError(t, err)

	require.Equal(t, []string{"building"}, collect(t, first, 1))
	require.Equal(t, []string{"building"}, collect(t, second, 1))
}

// Following a deployment that has not written anything yet is the normal case:
// the console opens the stream as the deploy starts.
func TestFollowingADeploymentThatHasNotStartedYetWorks(t *testing.T) {
	store := deploy.NewLogStore()

	backlog, ch, cancel := store.Follow("dep_not_yet")
	defer cancel()
	require.Empty(t, backlog)

	_, err := io.WriteString(store.Writer("dep_not_yet"), "started\n")
	require.NoError(t, err)
	require.Equal(t, []string{"started"}, collect(t, ch, 1))
}

func TestClosingTheSinkClosesEveryFollower(t *testing.T) {
	store := deploy.NewLogStore()
	w := store.Writer("dep_01HQ8")

	_, ch, cancel := store.Follow("dep_01HQ8")
	defer cancel()

	require.NoError(t, w.Close())

	select {
	case _, open := <-ch:
		require.False(t, open, "the stream ends rather than hanging")
	case <-time.After(2 * time.Second):
		t.Fatal("the follower was never closed")
	}
}

// Following a finished deploy hands back the whole log and a channel that is
// already closed, so a client does not wait for lines that will never come.
func TestFollowingAFinishedDeployReturnsTheLogAndAClosedChannel(t *testing.T) {
	store := deploy.NewLogStore()
	w := store.Writer("dep_01HQ8")
	_, err := io.WriteString(w, "done\n")
	require.NoError(t, err)
	require.NoError(t, w.Close())

	backlog, ch, cancel := store.Follow("dep_01HQ8")
	defer cancel()
	require.Equal(t, []string{"done"}, backlog)

	_, open := <-ch
	require.False(t, open)
}

func TestCloseIsIdempotent(t *testing.T) {
	store := deploy.NewLogStore()
	w := store.Writer("dep_01HQ8")

	require.NoError(t, w.Close())
	require.NoError(t, w.Close(), "a second Close does not close an already-closed channel")
}

func TestCancelStopsOneFollowerAndLeavesTheRest(t *testing.T) {
	store := deploy.NewLogStore()
	w := store.Writer("dep_01HQ8")

	_, leaving, cancelLeaving := store.Follow("dep_01HQ8")
	_, staying, cancelStaying := store.Follow("dep_01HQ8")
	defer cancelStaying()

	cancelLeaving()
	_, open := <-leaving
	require.False(t, open)

	_, err := io.WriteString(w, "still building\n")
	require.NoError(t, err)
	require.Equal(t, []string{"still building"}, collect(t, staying, 1))

	require.NotPanics(t, cancelLeaving, "cancelling twice is not a double close")
}

// In memory and bounded: this is the live tail a console watches, not the log
// retention R-222/R-223 govern.
func TestTheBacklogIsBoundedAndKeepsTheMostRecentLines(t *testing.T) {
	store := deploy.NewLogStore()
	w := store.Writer("dep_01HQ8")

	for i := range 2500 {
		_, err := fmt.Fprintf(w, "line %d\n", i)
		require.NoError(t, err)
	}

	backlog, _, cancel := store.Follow("dep_01HQ8")
	defer cancel()

	require.Len(t, backlog, 2000)
	require.Equal(t, "line 500", backlog[0], "the oldest are dropped")
	require.Equal(t, "line 2499", backlog[len(backlog)-1])
}

// A slow console must never slow a deploy: a listener that cannot keep up loses
// lines rather than blocking the build.
func TestASlowFollowerLosesLinesRatherThanBlockingTheBuild(t *testing.T) {
	store := deploy.NewLogStore()
	w := store.Writer("dep_01HQ8")

	_, ch, cancel := store.Follow("dep_01HQ8")
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 5000 {
			_, _ = fmt.Fprintf(w, "line %d\n", i)
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("writing blocked on a follower that was not reading")
	}

	// Whatever did fit is real output, not corrupted.
	select {
	case line := <-ch:
		require.Contains(t, line, "line ")
	default:
	}
}

func TestTwoDeploymentsDoNotShareAStream(t *testing.T) {
	store := deploy.NewLogStore()

	_, err := io.WriteString(store.Writer("dep_one"), "one\n")
	require.NoError(t, err)
	_, err = io.WriteString(store.Writer("dep_two"), "two\n")
	require.NoError(t, err)

	first, _, cancelFirst := store.Follow("dep_one")
	defer cancelFirst()
	second, _, cancelSecond := store.Follow("dep_two")
	defer cancelSecond()

	require.Equal(t, []string{"one"}, first)
	require.Equal(t, []string{"two"}, second)
}

// Two writers for the same deployment append to one stream: a build and the
// deploy that follows it are one log to whoever is watching.
func TestTwoWritersForOneDeploymentShareTheStream(t *testing.T) {
	store := deploy.NewLogStore()

	_, err := io.WriteString(store.Writer("dep_01HQ8"), "building\n")
	require.NoError(t, err)
	_, err = io.WriteString(store.Writer("dep_01HQ8"), "starting\n")
	require.NoError(t, err)

	backlog, _, cancel := store.Follow("dep_01HQ8")
	defer cancel()
	require.Equal(t, []string{"building", "starting"}, backlog)
}

func TestTheSinkIsAWriteCloser(t *testing.T) {
	var _ io.WriteCloser = deploy.NewLogStore().Writer("dep_01HQ8")
}
