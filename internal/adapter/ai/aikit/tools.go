package aikit

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/bemeek-io/pando/internal/adapter/api"
)

// The tools a repair, an answering or a revision call gets: two to read the
// repository, one to answer.
//
// Answering through a tool rather than in prose is deliberate. The schema is
// the closed set from design 10 §3, so a malformed amendment is rejected before
// it reaches Pando, and the shape core validates is the shape the model was
// given. Parsing amendments out of prose would mean two descriptions of the
// same structure, kept in step by hand.
const (
	ToolListFiles      = "list_files"
	ToolReadFile       = "read_file"
	ToolSubmitFindings = "submit_findings"

	// ToolSubmit is the one tool the administrative functions get.
	ToolSubmit = "submit"
)

// Tool is one tool, as every provider's tool definition needs it: a name, a
// description, and a JSON Schema object for its input.
type Tool struct {
	Name        string
	Description string

	// Properties and Required are the input schema's; the schema is always an
	// object.
	Properties map[string]any
	Required   []string

	// Strict asks a provider that can to validate the input against the
	// schema before the call reaches Pando.
	Strict bool
}

// Schema is the tool's input schema as a whole JSON Schema object.
func (t Tool) Schema() map[string]any {
	s := map[string]any{"type": "object", "properties": t.Properties, "required": t.Required}
	if t.Strict {
		s["additionalProperties"] = false
	}
	return s
}

// ScreenTools are the tools for fn: list, read, and submit findings.
func ScreenTools(fn api.AIFunction, questions []api.Question, values []string) []Tool {
	list := Tool{
		Name: ToolListFiles,
		Description: "List files in the repository matching a glob pattern, relative to the repository root. " +
			"Use this to find files before reading them. Listing does not count against the " +
			"read budget. Examples: \"*\", \"src/*\", \"**/Dockerfile\".",
		Properties: map[string]any{
			"pattern": map[string]any{"type": "string", "description": "A glob pattern relative to the repository root."},
		},
		Required: []string{"pattern"},
	}
	read := Tool{
		Name: ToolReadFile,
		Description: "Read one file from the repository, relative to the repository root. Each distinct " +
			"file counts against this screening's budget; re-reading one does not. Long files " +
			"are truncated.",
		Properties: map[string]any{
			"path": map[string]any{"type": "string", "description": "A path relative to the repository root."},
		},
		Required: []string{"path"},
	}
	submit := Tool{
		Name: ToolSubmitFindings,
		Description: "Submit your result. Call this exactly once, at the end. Submit an empty " +
			"amendments list when there is nothing the repository supports changing — that is " +
			"a good outcome, not a failure.",
		Properties: map[string]any{
			"amendments": map[string]any{
				"type":        "array",
				"description": "Changes to the deployment plan. Empty if none are needed.",
				"items":       itemSchema(fn, questions, values),
			},
			"notes": map[string]any{
				"type": "array",
				"description": "Observations worth telling the person reviewing this plan that " +
					"do not change it. Optional, and usually empty.",
				"items": map[string]any{"type": "string"},
			},
		},
		Required: []string{"amendments"},
		Strict:   true,
	}
	// A revision answers a person, so it says something back — required, so a
	// person who asked is never met with silence.
	if fn == api.AIFunctionRevisePlan {
		submit.Properties["reply"] = map[string]any{
			"type": "string",
			"description": "Your answer to the person, in one to three plain sentences: what you changed " +
				"and the file that shows it, or why the repository does not support what they asked. " +
				"No apology, no exclamation mark.",
		}
		submit.Required = []string{"amendments", "reply"}
	}
	return []Tool{list, read, submit}
}

// Call runs one read tool and says what to hand back: the text, and whether it
// is an error.
//
// A budget refusal comes back as an ordinary result rather than an error, so
// the model can submit what it has instead of the whole screening being lost
// over one file too many. That is the difference between a bounded screening
// and a failed one.
func Call(name, input string, src *Reader) (string, bool) {
	switch name {
	case ToolListFiles:
		var in struct {
			Pattern string `json:"pattern"`
		}
		if err := json.Unmarshal([]byte(input), &in); err != nil {
			return "That request could not be read: " + err.Error(), true
		}
		matches, err := src.Glob(in.Pattern)
		if err != nil {
			return err.Error(), true
		}
		if len(matches) == 0 {
			return "Nothing in this repository matches that pattern.", false
		}
		body, _ := json.Marshal(matches) //nolint:errcheck // a []string always marshals.
		return string(body), false

	case ToolReadFile:
		var in struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal([]byte(input), &in); err != nil {
			return "That request could not be read: " + err.Error(), true
		}
		body, err := src.Open(in.Path)
		if err != nil {
			if errors.Is(err, ErrBudget) {
				return err.Error() + ". Submit your findings now, based on what you have read.", false
			}
			return err.Error(), true
		}
		return body, false

	default:
		return "There is no tool by that name.", true
	}
}

// Findings reads a submit_findings call's input.
//
// Parsed rather than matched on the raw string: escaping in a tool input is
// the model's to choose, and string matching on it is how that bites.
func Findings(input string, src *Reader, model string) (api.ScreenResult, error) {
	var in struct {
		Amendments []api.Amendment `json:"amendments"`
		Notes      []string        `json:"notes"`
		Reply      string          `json:"reply"`
	}
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return api.ScreenResult{}, fmt.Errorf("the submitted result could not be read: %w", err)
	}
	return api.ScreenResult{
		Amendments: in.Amendments,
		Notes:      in.Notes,
		Reply:      in.Reply,
		FilesRead:  src.Files(),
		Model:      model,
	}, nil
}

// Preloaded reads the files an earlier call on this proposal read, and hands
// them over at the start: the model begins knowing what it knew last time,
// rather than listing and reading its way back there one round trip at a time.
// Read through the budgeted reader, so they count as reads and are recorded
// as sent (R-337); one the budget refuses, or that is gone, is left out.
func Preloaded(src *Reader, known []string) string {
	if len(known) == 0 {
		return ""
	}
	var b strings.Builder
	for _, name := range known {
		body, err := src.Open(name)
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "\n### %s\n\n```\n%s\n```\n", name, body)
	}
	if b.Len() == 0 {
		return ""
	}
	return "\n## Files you read about this plan before\n\nYou read these in an earlier look at this " +
		"repository, at this same commit. Their contents are below, so there is no need to read them " +
		"again; read anything else you need.\n" + b.String()
}

// Limit takes the lower of what core asked for and what the adapter will do.
// Neither side raises the other's (design 10 §2).
func Limit[T int | int64](asked, own T) T {
	if asked <= 0 || asked > own {
		return own
	}
	return asked
}
