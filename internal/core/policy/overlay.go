package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Host policy set in the startup configuration (R-271).
//
// An operator may fix any policy field in the config file or the environment.
// A field fixed that way overrides the stored document, everywhere policy is
// evaluated, and cannot be changed from the console, the API or the CLI while
// it is set — the answer to "why won't this setting change" is then the
// startup config, and GET /config says which file or variable.
//
// The stored document is never written with a fixed value: remove the setting
// from the config and restart, and what was stored before is what applies.

// Source is where a fixed value was set, as the config package reports it.
type Source struct {
	Kind string `json:"kind"`
	Name string `json:"name,omitempty"`
	Key  string `json:"key,omitempty"`
}

// Fixed is one policy field set at startup.
type Fixed struct {
	Key    string          `json:"key"`
	Value  json.RawMessage `json:"value"`
	Source Source          `json:"source"`
}

// Overlay is the policy fields set at startup.
type Overlay struct {
	fixed map[string]Fixed
}

// Setting is one field as the config gives it: a string from the environment,
// or a typed value from the config file.
type Setting struct {
	Key    string
	Value  any
	Source Source
}

// fields maps each policy field's JSON name to its Go type.
var fields = func() map[string]reflect.Type {
	out := map[string]reflect.Type{}
	t := reflect.TypeOf(Document{})
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			out[name] = t.Field(i).Type
		}
	}
	return out
}()

// Fields is every policy field's name, sorted.
func Fields() []string {
	out := make([]string, 0, len(fields))
	for name := range fields {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// NewOverlay checks each setting against the policy document and returns the
// overlay. A setting that is not a policy field, or will not read as one, is
// an error that stops startup: a misspelt or malformed policy is a policy that
// silently does not apply, which is worse than not starting.
func NewOverlay(settings []Setting) (*Overlay, error) {
	o := &Overlay{fixed: map[string]Fixed{}}
	for _, s := range settings {
		t, ok := fields[s.Key]
		if !ok {
			return nil, fmt.Errorf("%s sets %q, which is not a host policy setting. The settings are: %s",
				describe(s.Source), s.Key, strings.Join(Fields(), ", "))
		}
		raw, err := normalize(t, s.Value)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", describe(s.Source), err)
		}
		o.fixed[s.Key] = Fixed{Key: s.Key, Value: raw, Source: s.Source}
	}
	return o, nil
}

// normalize turns a setting into the JSON its field reads, checking it does.
func normalize(t reflect.Type, value any) (json.RawMessage, error) {
	if s, isString := value.(string); isString && t.Kind() != reflect.String {
		v, err := fromString(t, s)
		if err != nil {
			return nil, err
		}
		value = v
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	into := reflect.New(t)
	if err := json.Unmarshal(raw, into.Interface()); err != nil {
		return nil, fmt.Errorf("%s is not a valid %s", raw, kindName(t))
	}
	// A field whose type knows its own values says so here, at startup —
	// public_sharing: "sometimes" would otherwise be stored and mean nothing.
	if v, ok := into.Elem().Interface().(interface{ Valid() error }); ok {
		if err := v.Valid(); err != nil {
			return nil, err
		}
	}
	return json.Marshal(into.Elem().Interface())
}

// fromString reads an environment variable's text as a field's type: lists are
// comma-separated, booleans are true or false, numbers are numbers.
func fromString(t reflect.Type, s string) (any, error) {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Slice:
		items := []string{}
		for _, item := range strings.Split(s, ",") {
			if item = strings.TrimSpace(item); item != "" {
				items = append(items, item)
			}
		}
		return items, nil
	case reflect.Bool:
		b, err := strconv.ParseBool(strings.TrimSpace(s))
		if err != nil {
			return nil, fmt.Errorf("%q is not true or false", s)
		}
		return b, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a whole number", s)
		}
		return n, nil
	}
	return s, nil
}

func kindName(t reflect.Type) string {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Slice:
		return "list"
	case reflect.Bool:
		return "true or false"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return "whole number"
	}
	return t.Kind().String()
}

func describe(s Source) string {
	switch s.Kind {
	case "env":
		return "The environment variable " + s.Name
	case "file":
		return fmt.Sprintf("The config file %s, at %s,", s.Name, s.Key)
	}
	return "The startup configuration"
}

// Fixed lists the fields set at startup, sorted by name. Nil-safe.
func (o *Overlay) Fixed() []Fixed {
	if o == nil {
		return nil
	}
	out := make([]Fixed, 0, len(o.fixed))
	for _, f := range o.fixed {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Apply returns doc with the fixed fields laid over it. Nil-safe.
func (o *Overlay) Apply(doc Document) Document {
	if o == nil || len(o.fixed) == 0 {
		return doc
	}
	return o.copyFrom(doc, func(key string) json.RawMessage { return o.fixed[key].Value })
}

// Restore returns incoming with every fixed field put back to what stored
// holds, so a save never writes a startup value into the stored document.
func (o *Overlay) Restore(incoming, stored Document) Document {
	if o == nil || len(o.fixed) == 0 {
		return incoming
	}
	was := asMap(stored)
	return o.copyFrom(incoming, func(key string) json.RawMessage { return was[key] })
}

// Changes reports the first fixed field that incoming sets differently from
// its startup value, with that value's source. Fields left as the startup
// value — which is what GET /policy hands back — are not a change.
func (o *Overlay) Changes(incoming Document) (Fixed, bool) {
	if o == nil {
		return Fixed{}, false
	}
	now := asMap(incoming)
	for _, f := range o.Fixed() {
		if !sameJSON(now[f.Key], f.Value, fields[f.Key]) {
			return f, true
		}
	}
	return Fixed{}, false
}

func (o *Overlay) copyFrom(doc Document, value func(key string) json.RawMessage) Document {
	m := asMap(doc)
	for key := range o.fixed {
		if v := value(key); v != nil {
			m[key] = v
		} else {
			delete(m, key)
		}
	}
	raw, _ := json.Marshal(m)
	var out Document
	_ = json.Unmarshal(raw, &out)
	return out
}

func asMap(doc Document) map[string]json.RawMessage {
	raw, _ := json.Marshal(doc)
	m := map[string]json.RawMessage{}
	_ = json.Unmarshal(raw, &m)
	return m
}

// sameJSON compares two encodings of one field, treating absent as the zero
// value — omitempty drops zeros, so "not there" and "0" are the same setting.
func sameJSON(a, b json.RawMessage, t reflect.Type) bool {
	read := func(raw json.RawMessage) any {
		v := reflect.New(t)
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, v.Interface())
		}
		return v.Elem().Interface()
	}
	x, y := read(a), read(b)
	if t.Kind() == reflect.Slice && reflect.ValueOf(x).Len() == 0 && reflect.ValueOf(y).Len() == 0 {
		return true
	}
	return reflect.DeepEqual(x, y)
}

// Store is where the policy document is kept.
type Store interface {
	Load(ctx context.Context) (Document, error)
	Save(ctx context.Context, doc Document, updatedBy string) error
}

// Wrap returns a store that reads the effective document — the stored one
// with the startup fields over it — and saves without writing a startup value
// into the stored one. Everything that reads policy goes through it, so no
// reader can see a stored value the startup config has replaced.
func (o *Overlay) Wrap(s Store) Store { return overlaid{o: o, s: s} }

type overlaid struct {
	o *Overlay
	s Store
}

func (w overlaid) Load(ctx context.Context) (Document, error) {
	doc, err := w.s.Load(ctx)
	if err != nil {
		return doc, err
	}
	return w.o.Apply(doc), nil
}

func (w overlaid) Save(ctx context.Context, doc Document, updatedBy string) error {
	if w.o != nil && len(w.o.fixed) > 0 {
		stored, err := w.s.Load(ctx)
		if err != nil {
			return err
		}
		doc = w.o.Restore(doc, stored)
	}
	return w.s.Save(ctx, doc, updatedBy)
}
