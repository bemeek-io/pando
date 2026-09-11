package secret

import (
	"encoding/json"
	"fmt"

	"go.uber.org/zap/zapcore"
)

// Redacted is what a Value renders as, everywhere, always.
const Redacted = "[redacted]"

// Value holds a secret and refuses to render it.
//
// R-194 is enforced structurally rather than by review: a secret cannot be
// accidentally logged, serialized, or formatted into an error string, because
// the type will not print. Getting the value out requires calling Reveal, which
// is deliberately conspicuous and greppable.
//
// The zero Value is valid and empty.
type Value struct {
	// Unexported, and not a named type an outside package can reach. Struct
	// embedding a Value into a logged struct therefore cannot leak it either.
	v string
}

// New wraps a secret.
func New(s string) Value { return Value{v: s} }

// Reveal returns the underlying secret. Every call site is a place a secret
// escapes the type's protection, so they should be few and obvious. Core calls
// it during plan resolution to populate WorkloadPlan.Env, and essentially
// nowhere else.
func (v Value) Reveal() string { return v.v }

// IsZero reports whether the Value holds nothing. Useful for validation without
// revealing.
func (v Value) IsZero() bool { return v.v == "" }

// Len returns the length of the secret. Safe to log: it discloses nothing an
// attacker cannot infer, and it makes "the secret is empty" debuggable without
// a Reveal.
func (v Value) Len() int { return len(v.v) }

// String implements fmt.Stringer. Covers %s, %v, and any code path that
// stringifies a value.
func (v Value) String() string { return Redacted }

// GoString implements fmt.GoStringer. Without it, %#v prints the struct's
// fields verbatim and defeats String entirely — the most easily overlooked leak
// path of the set, because %#v is what people reach for when debugging.
func (v Value) GoString() string { return Redacted }

// Format implements fmt.Formatter, which takes precedence over Stringer and
// GoStringer for every verb. It exists so that no verb — including ones that
// would otherwise reach the struct directly, such as %d or %x on a
// misused value — can render the secret.
func (v Value) Format(f fmt.State, verb rune) {
	_, _ = f.Write([]byte(Redacted))
}

// MarshalJSON implements json.Marshaler. A Value inside an error envelope,
// audit detail, or API response serializes as the redaction string.
func (v Value) MarshalJSON() ([]byte, error) { return json.Marshal(Redacted) }

// UnmarshalJSON implements json.Unmarshaler, so a Value can be decoded from a
// request body. Decoding the redaction string yields an empty Value rather than
// the literal text — round-tripping a redacted payload must not set a secret to
// "[redacted]".
func (v *Value) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	if s == Redacted {
		v.v = ""
		return nil
	}
	v.v = s
	return nil
}

// MarshalText implements encoding.TextMarshaler, for libraries that reach for
// it instead of json.Marshaler.
func (v Value) MarshalText() ([]byte, error) { return []byte(Redacted), nil }

// MarshalLogObject implements zapcore.ObjectMarshaler, so zap.Object and
// zap.Any redact rather than reflecting over the struct.
func (v Value) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddString("value", Redacted)
	return nil
}

// Compile-time proof that every rendering path is covered. If a future refactor
// drops one of these, the build breaks rather than a secret reaching a log.
var (
	_ fmt.Stringer            = Value{}
	_ fmt.GoStringer          = Value{}
	_ fmt.Formatter           = Value{}
	_ json.Marshaler          = Value{}
	_ json.Unmarshaler        = (*Value)(nil)
	_ zapcore.ObjectMarshaler = Value{}
)
