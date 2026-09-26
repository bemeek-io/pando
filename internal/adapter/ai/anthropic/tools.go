package anthropic

import (
	"github.com/anthropics/anthropic-sdk-go"

	"github.com/bemeek-io/pando/internal/adapter/api"
)

// The three tools a repair or an answering call gets: two to read the repository, one to answer.
//
// Answering through a tool rather than in prose is deliberate. The schema is
// the closed set from design 10 §3, so a malformed amendment is rejected before
// it reaches Pando, and the shape core validates is the shape the model was
// given. Parsing amendments out of prose would mean two descriptions of the
// same structure, kept in step by hand.
const (
	toolListFiles      = "list_files"
	toolReadFile       = "read_file"
	toolSubmitFindings = "submit_findings"
)

func tools(fn api.AIFunction, questions []api.Question) []anthropic.ToolUnionParam {
	list := anthropic.ToolParam{
		Name: toolListFiles,
		Description: anthropic.String(
			"List files in the repository matching a glob pattern, relative to the repository root. " +
				"Use this to find files before reading them. Listing does not count against the " +
				"read budget. Examples: \"*\", \"src/*\", \"**/Dockerfile\"."),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: map[string]any{
				"pattern": map[string]any{
					"type":        "string",
					"description": "A glob pattern relative to the repository root.",
				},
			},
			Required: []string{"pattern"},
		},
	}

	read := anthropic.ToolParam{
		Name: toolReadFile,
		Description: anthropic.String(
			"Read one file from the repository, relative to the repository root. Each distinct " +
				"file counts against this screening's budget; re-reading one does not. Long files " +
				"are truncated."),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "A path relative to the repository root.",
				},
			},
			Required: []string{"path"},
		},
	}

	submit := anthropic.ToolParam{
		Name: toolSubmitFindings,
		Description: anthropic.String(
			"Submit your result. Call this exactly once, at the end. Submit an empty " +
				"amendments list when there is nothing the repository supports changing — that is " +
				"a good outcome, not a failure."),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: map[string]any{
				"amendments": map[string]any{
					"type":        "array",
					"description": "Changes to the deployment plan. Empty if none are needed.",
					"items":       itemSchema(fn, questions),
				},
				"notes": map[string]any{
					"type": "array",
					"description": "Observations worth telling the person reviewing this plan that " +
						"do not change it. Optional, and usually empty.",
					"items": map[string]any{"type": "string"},
				},
			},
			Required: []string{"amendments"},
		},
	}

	// A revision answers a person, so it says something back — required, so a
	// person who asked is never met with silence.
	if fn == api.AIFunctionRevisePlan {
		submit.InputSchema.Properties.(map[string]any)["reply"] = map[string]any{
			"type": "string",
			"description": "Your answer to the person, in one to three plain sentences: what you changed " +
				"and the file that shows it, or why the repository does not support what they asked. " +
				"No apology, no exclamation mark.",
		}
		submit.InputSchema.Required = []string{"amendments", "reply"}
	}

	// strict: true, so the API validates against the schema before Pando sees
	// the call. additionalProperties and required are what make that possible.
	submit.Strict = anthropic.Bool(true)
	submit.InputSchema.ExtraFields = map[string]any{"additionalProperties": false}

	return []anthropic.ToolUnionParam{
		{OfTool: &list}, {OfTool: &read}, {OfTool: &submit},
	}
}

// itemSchema is what one submitted amendment may look like for fn.
//
// A repair gets the whole closed set. Answering questions gets a schema of its
// own: one kind, and a key that must be one of the questions actually asked,
// with every field required. Core refuses anything else from that call
// regardless (R-336), but the answer schema used to be the general one, where
// "key" is optional because most kinds have none — and a model answered the
// build_strategy question correctly with no key at all, so the answer was
// refused and the person was asked anyway.
func itemSchema(fn api.AIFunction, questions []api.Question) map[string]any {
	if fn != api.AIFunctionAnswerQuestions {
		return amendmentSchema([]string{
			"set_command", "set_env", "set_port", "set_health", "add_slot",
			"set_build_context", "set_dockerfile", "set_static_dir",
			"add_volume", "answer_question", "add_warning",
		})
	}

	keys := make([]string, 0, len(questions))
	for _, q := range questions {
		keys = append(keys, q.Key)
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"kind", "key", "value", "reason", "evidence"},
		"properties": map[string]any{
			"kind": map[string]any{
				"type": "string",
				"enum": []string{string(api.AmendAnswerQuestion)},
			},
			"key": map[string]any{
				"type":        "string",
				"enum":        keys,
				"description": "The key of the question this answers, exactly as listed.",
			},
			"value": map[string]any{
				"type": "string",
				"description": "The answer, as a person would type it into the question's field: " +
					"one of its valid answers when it lists them.",
			},
			"reason": map[string]any{
				"type": "string",
				"description": "Why the repository answers the question this way, written so that " +
					"someone who cannot see the repository understands it. One or two sentences.",
			},
			"evidence": map[string]any{
				"type":     "array",
				"items":    map[string]any{"type": "string"},
				"minItems": 1,
				"description": "The repository paths you read that settle this, such as " +
					"\"docker-compose.yml\". Each is checked to exist; an empty path is not evidence.",
			},
		},
	}
}

// amendmentSchema is the closed set, as JSON Schema.
//
// It mirrors api.Amendment, and the mirroring is the point: a kind that is not
// in this enum cannot be asked for, which is R-332 arriving one layer earlier
// than core's validation rather than instead of it.
func amendmentSchema(kinds []string) map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"kind", "reason", "evidence"},
		"properties": map[string]any{
			"kind": map[string]any{
				"type":        "string",
				"enum":        kinds,
				"description": "Which change this is.",
			},
			"workload": map[string]any{
				"type": "string",
				"description": "Which workload this applies to. Leave empty for the primary one, " +
					"which is almost always correct.",
			},
			"key": map[string]any{
				"type": "string",
				"description": "An environment variable name for set_env, a dependency key for " +
					"add_slot, or the question key for answer_question.",
			},
			"value": map[string]any{
				"type": "string",
				"description": "The value: the variable's value for set_env, the answer for " +
					"answer_question, the message for add_warning, or a shell command line for " +
					"set_command when command is not given.",
			},
			"command": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "The command as an argv, for set_command.",
			},
			"path": map[string]any{
				"type": "string",
				"description": "A path in the repository for set_build_context, set_dockerfile " +
					"and set_static_dir; the health check path for set_health; the absolute " +
					"container path for add_volume.",
			},
			"port": map[string]any{
				"type":        "integer",
				"description": "A port number, for set_port and set_health.",
			},
			"slot_type": map[string]any{
				"type":        "string",
				"enum":        []string{"postgres", "mysql", "redis", "s3", "smtp", "unknown"},
				"description": "What kind of dependency, for add_slot.",
			},
			"required": map[string]any{
				"type": "boolean",
				"description": "Whether the app cannot start without this dependency. Only " +
					"honored when the trial run actually crashed; otherwise the dependency is " +
					"recorded as optional so it cannot block a deploy.",
			},
			"reason": map[string]any{
				"type": "string",
				"description": "Why this change is needed, written so that someone who cannot see " +
					"the repository can understand it. No apology, no error prefix. One or two " +
					"sentences.",
			},
			"evidence": map[string]any{
				"type":     "array",
				"items":    map[string]any{"type": "string"},
				"minItems": 1,
				"description": "The repository paths this rests on. These are checked to exist; " +
					"an amendment citing a file that is not in the repository is refused. Cite " +
					"only files you actually read.",
			},
		},
	}
}
