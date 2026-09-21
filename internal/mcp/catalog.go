package mcp

// The catalog, for the reference.
//
// The same list the server announces, not a copy of it: an agent's view of
// Pando and the documentation of that view cannot disagree, because there is
// one list. A tool added to `toolList` appears in the console's API screen and
// in `docs/mcp.md` with no further step.

// ToolDoc is one tool as the reference publishes it.
type ToolDoc struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Schema      map[string]any `json:"schema"`
}

// Catalog returns the tools this server offers, in the order it offers them.
func Catalog() []ToolDoc {
	out := make([]ToolDoc, 0, len(toolList))
	for _, t := range toolList {
		out = append(out, ToolDoc{Name: t.Name, Description: t.Description, Schema: t.Schema})
	}
	return out
}
