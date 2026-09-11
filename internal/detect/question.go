package detect

import (
	"fmt"
	"strings"

	"github.com/bemeek-io/pando/internal/errs"
)

// Validate checks a question against R-105.
//
// R-105 is a content requirement, not a style note. The expected workflow is a
// person pasting the question into the assistant that wrote their app — so the
// question has to be answerable by a model that cannot see the repository, and
// cannot see Pando either.
//
// The design gives the bar directly:
//
//	"Which port?" fails.
//	"This app appears to be a Node.js service. Pando could not determine which
//	 port it serves HTTP on. Valid answer: a port number such as 3000." passes.
//
// The checks below are necessary conditions, not sufficient ones — no linter
// can confirm a question is genuinely answerable. They catch the failures that
// actually happen: a bare fragment, a question that assumes the reader can see
// the repo, and one that never says what a good answer looks like.
func (q Question) Validate() error {
	var problems []string

	if strings.TrimSpace(q.Key) == "" {
		problems = append(problems, "it has no key, so an answer could not be matched back to it")
	}

	prompt := strings.TrimSpace(q.Prompt)
	switch {
	case prompt == "":
		problems = append(problems, "it has no prompt")
	case len(prompt) < 60:
		// "Which port?" is 12 characters. A question that stands on its own —
		// context, what is unknown, what a valid answer is — does not fit in a
		// fragment.
		problems = append(problems, fmt.Sprintf(
			"the prompt is %d characters, which is too short to be self-contained; "+
				"it needs to say what Pando found, what it could not determine, and what a valid answer looks like",
			len(prompt)))
	}

	// The reader cannot see the repository, so the question must describe what
	// was found rather than pointing at it.
	for _, deictic := range []string{"this file", "that file", "the above", "here", "as shown"} {
		if strings.Contains(strings.ToLower(prompt), deictic) {
			problems = append(problems, fmt.Sprintf(
				"the prompt says %q, which assumes the reader can see something Pando is looking at", deictic))
		}
	}

	// Without this, an assistant has to guess the shape of the answer, and a
	// wrong shape is worse than no answer — it deploys something broken.
	if prompt != "" && !mentionsValidAnswer(prompt) && len(q.Options) == 0 {
		problems = append(problems, "the prompt never says what a valid answer looks like, "+
			"so an assistant answering it has to guess the format")
	}

	if strings.TrimSpace(q.Why) == "" {
		problems = append(problems, "it does not say why Pando is asking, "+
			"which is what the console shows beside the question")
	}

	if q.Kind == QuestionKindChoice() && len(q.Options) == 0 {
		problems = append(problems, "it is a choice with nothing to choose from")
	}

	if len(problems) == 0 {
		return nil
	}
	return errs.Newf(errs.ValidInvalid,
		"A detection question does not meet the standard in R-105: %s.", strings.Join(problems, "; ")).
		WithDetail("key", q.Key).
		WithDetail("prompt", q.Prompt).
		WithRemedy("A question must be answerable by someone — or something — that cannot see this repository. " +
			"Say what Pando found, what it could not work out, and what a valid answer looks like.")
}

// mentionsValidAnswer reports whether the prompt tells the reader what shape an
// answer takes.
func mentionsValidAnswer(prompt string) bool {
	lower := strings.ToLower(prompt)
	for _, phrase := range []string{"valid answer", "for example", "such as", "e.g."} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

// ValidateAll checks every question, reporting all failures at once.
//
// Accumulating rather than stopping at the first is deliberate: this runs in
// tests and in review, and someone fixing detection deserves the whole list.
func ValidateAll(questions []Question) error {
	var problems []map[string]any
	for _, q := range questions {
		if err := q.Validate(); err != nil {
			e := errs.As(err)
			problems = append(problems, map[string]any{"key": q.Key, "problem": e.Message})
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return errs.Newf(errs.ValidInvalid,
		"%d detection questions do not meet the standard in R-105.", len(problems)).
		WithDetail("questions", problems)
}
