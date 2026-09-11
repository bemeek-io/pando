//go:build integration

// The detection corpus.
//
// "Detection quality below the R-103 bar" is the top risk in the register, and
// its mitigation is this: real repositories, checked early, with
// questions-per-deploy tracked as a number rather than a feeling. A detector
// that improves on the repository in front of you and regresses everywhere else
// is the failure this exists to catch.
//
//	make detection-corpus
package corpus_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/core/source"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/detect"
)

type manifest struct {
	Budgets struct {
		QuestionsPerDeployMean float64 `json:"questions_per_deploy_mean"`
		QuestionsPerDeployMax  int     `json:"questions_per_deploy_max"`
		MustDetectStrategy     float64 `json:"must_detect_strategy"`
	} `json:"budgets"`
	Cases []testCase `json:"cases"`
}

type testCase struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Ref    string `json:"ref"`
	Subdir string `json:"subdir"`
	Why    string `json:"why"`
	Expect struct {
		Strategy         string   `json:"strategy"`
		MinConfidence    float64  `json:"min_confidence"`
		MaxConfidence    float64  `json:"max_confidence"`
		Questions        int      `json:"questions"`
		EvidenceContains string   `json:"evidence_contains"`
		ExpectSlots      []string `json:"expect_slots"`
		ExpectVolumes    bool     `json:"expect_volumes"`
	} `json:"expect"`
}

type outcome struct {
	Case       string
	Strategy   string
	Confidence float64

	// Questions is what a person is asked. Deferred is what the trial run is
	// expected to answer on their behalf (R-097). Only the first is a cost the
	// user pays, and only the first is held against the budget — but both are
	// printed, so deferring a question cannot be used to hide it.
	Questions int
	Deferred  int

	Budget           int
	ExpectedDetected bool
	Failures         []string
}

// TestDetectionCorpus runs detection against real repositories.
//
// Each case reports rather than stopping at the first failure, because the
// number that matters is the aggregate: a corpus where one case regressed and
// nine improved is a different situation from one where nine regressed, and a
// run that stops at the first tells you neither.
func TestDetectionCorpus(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("corpus.json"))
	require.NoError(t, err)

	var m manifest
	require.NoError(t, json.Unmarshal(raw, &m))
	require.NotEmpty(t, m.Cases)

	auction := detect.NewAuction(
		detect.DockerfileDetector{},
		detect.ComposeDetector{},
		detect.StaticDetector{},
		detect.BuildpackDetector{},
		detect.MonorepoDetector{},
	)

	var results []outcome
	for _, c := range m.Cases {
		results = append(results, runCase(t, auction, c))
	}

	report(t, m, results)
}

func runCase(t *testing.T, auction *detect.Auction, c testCase) outcome {
	t.Helper()
	out := outcome{
		Case:             c.Name,
		Budget:           c.Expect.Questions,
		ExpectedDetected: c.Expect.Strategy != "" && c.Expect.Strategy != "unknown",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	checkout, err := source.Fetch(ctx, spec.Source{
		Type: spec.SourceGit, URL: c.URL, Ref: c.Ref, Subdir: c.Subdir,
	})
	if err != nil {
		out.Failures = append(out.Failures, "could not fetch: "+err.Error())
		return out
	}
	defer checkout.Close()

	result, err := auction.Run(ctx, checkout.View(c.Subdir))
	if err != nil {
		out.Failures = append(out.Failures, "detection failed: "+err.Error())
		return out
	}

	// A blocked result keeps the winning strategy, so without this check a
	// compose file Pando refused to import would look like one it imported.
	if result.Blocked != nil {
		out.Failures = append(out.Failures, "detection blocked: "+result.Blocked.Error())
	}

	out.Strategy = string(result.Winner.Strategy)
	out.Confidence = result.Winner.Confidence
	out.Questions = len(detect.Asked(result.Questions))
	out.Deferred = len(result.Questions) - out.Questions

	if c.Expect.Strategy != "" && out.Strategy != c.Expect.Strategy {
		out.Failures = append(out.Failures,
			fmt.Sprintf("detected %s, expected %s", out.Strategy, c.Expect.Strategy))
	}
	if c.Expect.MinConfidence > 0 && out.Confidence < c.Expect.MinConfidence {
		out.Failures = append(out.Failures,
			fmt.Sprintf("confidence %.2f below %.2f", out.Confidence, c.Expect.MinConfidence))
	}
	if c.Expect.MaxConfidence > 0 && out.Confidence > c.Expect.MaxConfidence {
		out.Failures = append(out.Failures,
			fmt.Sprintf("confidence %.2f above %.2f — too sure for what it can see",
				out.Confidence, c.Expect.MaxConfidence))
	}

	// The budget is a ceiling, not a target. Asking fewer is always better;
	// asking more is a barrier between someone and their running app.
	if out.Questions > c.Expect.Questions {
		out.Failures = append(out.Failures,
			fmt.Sprintf("asked %d questions, budget %d", out.Questions, c.Expect.Questions))
	}

	// Whatever it asks must be answerable by something that cannot see the repo.
	if err := detect.ValidateAll(result.Questions); err != nil {
		out.Failures = append(out.Failures, "R-105: "+err.Error())
	}

	for _, want := range c.Expect.ExpectSlots {
		if !hasSlotType(result.Winner.Draft.Slots, want) {
			out.Failures = append(out.Failures, "no "+want+" slot detected")
		}
	}
	if c.Expect.ExpectVolumes && len(result.Winner.Draft.Volumes) == 0 {
		out.Failures = append(out.Failures, "no volumes detected")
	}

	return out
}

// report prints the corpus table and asserts the aggregate budgets.
func report(t *testing.T, m manifest, results []outcome) {
	t.Helper()

	sort.Slice(results, func(i, j int) bool { return results[i].Case < results[j].Case })

	var totalQuestions, detected, expectedDetected, worst int
	t.Log("")
	t.Log("detection corpus")
	t.Log("--------------------------------------------------------------------")
	for _, r := range results {
		status := "ok  "
		if len(r.Failures) > 0 {
			status = "FAIL"
		}
		deferred := ""
		if r.Deferred > 0 {
			deferred = fmt.Sprintf("  (+%d deferred to trial run)", r.Deferred)
		}
		t.Logf("%s  %-28s %-10s conf %.2f  questions %d/%d%s",
			status, r.Case, r.Strategy, r.Confidence, r.Questions, r.Budget, deferred)
		for _, f := range r.Failures {
			t.Logf("        %s", f)
		}

		totalQuestions += r.Questions
		if r.Questions > worst {
			worst = r.Questions
		}

		// Rate is measured over the cases where a strategy was the right answer.
		// Two cases in this corpus are supposed to come back unknown — a
		// monorepo and an all-but-empty repository — and counting those as
		// detection failures would mean the metric goes up when detection gets
		// worse at recognizing that it does not know.
		if r.ExpectedDetected {
			expectedDetected++
			if r.Strategy != "" && r.Strategy != "unknown" {
				detected++
			}
		}
	}

	mean := float64(totalQuestions) / float64(len(results))
	rate := float64(detected) / float64(expectedDetected)

	t.Log("--------------------------------------------------------------------")
	t.Logf("questions per deploy: mean %.2f (budget %.2f), worst %d (budget %d)",
		mean, m.Budgets.QuestionsPerDeployMean, worst, m.Budgets.QuestionsPerDeployMax)
	t.Logf("strategy detected:    %.0f%% of %d cases where one was expected (budget %.0f%%)",
		rate*100, expectedDetected, m.Budgets.MustDetectStrategy*100)
	t.Log("")

	var failed []string
	for _, r := range results {
		if len(r.Failures) > 0 {
			failed = append(failed, r.Case)
		}
	}
	require.Empty(t, failed, "corpus cases failed: %v", failed)

	// The aggregate is the number that matters. A corpus that passes every case
	// while the mean creeps up is still a regression.
	require.LessOrEqual(t, mean, m.Budgets.QuestionsPerDeployMean,
		"mean questions per deploy is above budget — detection is asking more than it used to")
	require.LessOrEqual(t, worst, m.Budgets.QuestionsPerDeployMax)
	require.GreaterOrEqual(t, rate, m.Budgets.MustDetectStrategy)
}

func hasSlotType(slots []spec.Slot, want string) bool {
	for _, s := range slots {
		if string(s.Type) == want {
			return true
		}
	}
	return false
}
