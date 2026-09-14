package main

import (
	"bytes"
	"strings"
	"testing"
)

// The version command has two behaviors because there are two kinds of build:
// one made by the release workflow, which knows what it is, and every other
// one, which does not. A build that reports a version nobody stamped is worse
// than one that admits it is a development build, because the first is
// pasteable into a bug report and wrong.

func TestAnUnstampedBuildSaysSo(t *testing.T) {
	out := runVersion(t, "dev", "", "")
	if !strings.Contains(out, "development build") {
		t.Fatalf("want a development build, got %q", out)
	}
	if strings.Contains(out, "dev\n") {
		t.Fatalf("the sentinel leaked into the output: %q", out)
	}
}

func TestAStampedBuildReportsWhatItWasBuiltFrom(t *testing.T) {
	out := runVersion(t, "1.2.3", "9fb3c0de", "2026-09-13T00:00:00Z")
	for _, want := range []string{"pando 1.2.3", "commit 9fb3c0de", "built 2026-09-13T00:00:00Z"} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in output, got %q", want, out)
		}
	}
}

// A tarball built by hand with only -X main.buildVersion set is a real case,
// and it should print a version rather than an empty commit line.
func TestAVersionWithoutACommitPrintsTheVersionAlone(t *testing.T) {
	out := runVersion(t, "1.2.3", "", "")
	if got := strings.TrimSpace(out); got != "pando 1.2.3" {
		t.Fatalf("want %q, got %q", "pando 1.2.3", got)
	}
}

func runVersion(t *testing.T, version, commit, date string) string {
	t.Helper()

	buildVersion, buildCommit, buildDate = version, commit, date
	t.Cleanup(func() { buildVersion, buildCommit, buildDate = "dev", "", "" })

	var buf bytes.Buffer
	cmd := versionCmd()
	cmd.SetOut(&buf)
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("version: %v", err)
	}
	return buf.String()
}
