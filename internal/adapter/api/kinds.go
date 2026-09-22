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

	// Type is "string", "int" or "bool": how a form should ask for it and how
	// the value goes into the configuration's JSON.
	Type string `json:"type"`

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
