package api

// KindInfo describes a kind of adapter this build of Pando can run: what it is
// called, what it is for, and the settings configuring one takes.
//
// Data, like capabilities (R-254). Each adapter package describes itself, and
// the console and the CLI build their "add an adapter" forms from it — so a
// kind added to Pando is configurable from every surface without either being
// taught about it (R-261).
type KindInfo struct {
	Category    Category `json:"category"`
	Kind        string   `json:"kind"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Fields      []Field  `json:"fields"`

	// IDPrefix is the prefix an instance's ID conventionally takes, such as
	// "ai_" — so a form can suggest "ai_anthropic" rather than ask for one.
	IDPrefix string `json:"id_prefix"`
}

// Field is one setting.
type Field struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Help  string `json:"help,omitempty"`

	// Type is "string", "int", "bool" or "select": how a form should ask for
	// it and how the value goes into the configuration's JSON. A select's value
	// is a string.
	Type string `json:"type"`

	// Options are what a "select" offers.
	Options []Option `json:"options,omitempty"`

	// Other lets a "select" take a value not among Options, typed in — a DNS
	// provider Pando does not name, say.
	Other bool `json:"other,omitempty"`

	// Multiline asks for a text area: a credential given as KEY=value lines.
	Multiline bool `json:"multiline,omitempty"`

	// ShownWhen hides the setting unless another setting has one of the
	// given values — DNS provider fields only when certificates use DNS. A
	// form that ignores it still works; it only shows more than it needs to.
	ShownWhen *Condition `json:"shown_when,omitempty"`

	Required bool `json:"required,omitempty"`

	// Credential marks a secret such as an API key. It is sent in the create
	// request's write-only "credentials", stored encrypted, and never shown
	// again (R-190) — never in "config", which the database refuses it in.
	Credential bool `json:"credential,omitempty"`

	// Default is the value used when the setting is left empty — the
	// adapter's own default, stated so a form can show it rather than an
	// example that might be mistaken for it.
	Default string `json:"default,omitempty"`

	// Placeholder is an example value, for a setting with no default.
	Placeholder string `json:"placeholder,omitempty"`
}

// Option is one choice a "select" offers.
type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Condition is a setting's value a form checks before showing another.
type Condition struct {
	Key    string   `json:"key"`
	Values []string `json:"values"`
}
