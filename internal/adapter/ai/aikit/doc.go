// Package aikit is what every AI adapter does the same way, whatever its
// provider: the prompts, the tools a model is given and their schemas, the
// budgeted reader a model reads the repository through, and what a tool call
// does. An adapter translates these into its provider's own request types and
// runs the conversation; nothing here speaks to a provider.
//
// One copy, so the Anthropic, OpenAI and local adapters ask the same
// questions under the same rules (R-332 – R-334) and are refused the same way
// by core when a model breaks one. A second copy of a prompt is a copy that
// drifts.
//
// Like every package under internal/adapter, this imports adapter/api and
// nothing of core's authorization, audit or state (R-027).
package aikit

// MaxFileBytes is the most of one file a model is shown. Longer files are
// truncated, and say so.
const MaxFileBytes = 64 << 10
