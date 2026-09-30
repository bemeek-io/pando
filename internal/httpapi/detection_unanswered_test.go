package httpapi

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/trypando/pando/internal/core/state"
	"github.com/trypando/pando/internal/detect"
)

// A client can tell "waiting for answers" from "answered, ready to accept".
// Status stays needs_answers until accept, which is when answers are applied,
// so without this a client polling after answering saw no change (issue #55).
func TestR102_TheDetectionSaysWhichQuestionsAreStillOpen(t *testing.T) {
	body, err := json.Marshal(detect.Proposal{Questions: []detect.Question{
		{Key: "primary_port"}, {Key: "start_command"},
	}})
	require.NoError(t, err)
	d := state.Detection{Status: state.DetectionNeedsAnswers, Body: body,
		Answers: map[string]string{"primary_port": "8080"}}

	require.Equal(t, []string{"start_command"}, (&Server{}).detectionResponse(d)["unanswered"])

	d.Answers["start_command"] = "npm start"
	require.Equal(t, []string{}, (&Server{}).detectionResponse(d)["unanswered"], "answered, ready to accept")

	d.Status = state.DetectionReady
	_, present := (&Server{}).detectionResponse(d)["unanswered"]
	require.False(t, present, "only a detection waiting on answers says what it is waiting on")
}
