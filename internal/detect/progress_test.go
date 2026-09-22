package detect_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/detect"
)

// Detection reports its stages as it reaches them, and hands over what the
// auction found before the trial run starts — so the onboarding page can show
// the approach and the workloads while the slowest step is still going.
func TestDetectionReportsWhatItHasFoundAsItGoes(t *testing.T) {
	source := memSource{
		"Dockerfile":   "FROM node:20\nEXPOSE 3000\nCMD [\"node\", \"server.js\"]\n",
		"server.js":    "require('http').createServer().listen(3000)\n",
		".env.example": "API_URL=\n",
		"package.json": `{"name":"notes"}`,
	}

	var stages []string
	var partial *detect.Proposal
	ctx := detect.WithProgress(context.Background(), func(stage string, p *detect.Proposal) {
		stages = append(stages, stage)
		if stage == detect.StageTrying {
			partial = p
		}
	})

	job := &detect.Job{Auction: auction()}
	_, err := job.Run(ctx, "app_1", spec.Source{Type: "git", URL: "https://example.test/app"}, source)
	require.NoError(t, err)

	require.Equal(t, []string{detect.StageDetecting, detect.StageTrying}, stages)
	require.NotNil(t, partial, "the trying stage carries the auction's result")
	require.NotEmpty(t, partial.Winner.Detector)
	require.NotEmpty(t, partial.DraftSpec.Workloads, "with the workloads it will run")

	// With no listener, nothing is reported and nothing breaks.
	_, err = job.Run(context.Background(), "app_1", spec.Source{Type: "git", URL: "https://example.test/app"}, source)
	require.NoError(t, err)
}
