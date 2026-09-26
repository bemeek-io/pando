package anthropic

import (
	"github.com/anthropics/anthropic-sdk-go"

	"github.com/bemeek-io/pando/internal/adapter/ai/aikit"
	"github.com/bemeek-io/pando/internal/adapter/api"
)

// tools are aikit's screening tools as the Messages API takes them. The tools
// and their schemas are shared by every AI adapter; only this translation is
// Anthropic's.
func tools(fn api.AIFunction, questions []api.Question, values []string) []anthropic.ToolUnionParam {
	var out []anthropic.ToolUnionParam
	for _, t := range aikit.ScreenTools(fn, questions, values) {
		param := anthropic.ToolParam{
			Name:        t.Name,
			Description: anthropic.String(t.Description),
			InputSchema: anthropic.ToolInputSchemaParam{Properties: t.Properties, Required: t.Required},
		}
		// strict: true, so the API validates against the schema before Pando
		// sees the call. additionalProperties and required are what make that
		// possible.
		if t.Strict {
			param.Strict = anthropic.Bool(true)
			param.InputSchema.ExtraFields = map[string]any{"additionalProperties": false}
		}
		out = append(out, anthropic.ToolUnionParam{OfTool: &param})
	}
	return out
}
