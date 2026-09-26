// Package assist is which AI adapter performs each AI function (R-259), and
// the administrative functions built on that: drafting access and policy,
// searching the audit log, and answering from the reference (R-343 … R-346).
//
// It is the service layer both the HTTP API and the MCP server call (R-261).
// An adapter here proposes and never applies: every draft is checked against
// the closed set it was drawn from, every audit query is run by core under the
// caller's own authority, and nothing is created except by a person through
// the ordinary endpoints.
package assist

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/state"
	"github.com/bemeek-io/pando/internal/errs"
)

// Source is where an assignment was made: the database, or the startup
// configuration (R-271).
type Source struct {
	// Kind is "database" or "file".
	Kind string `json:"kind"`
	// Name is the config file's path.
	Name string `json:"name,omitempty"`
	// Key is the key inside the config file.
	Key string `json:"key,omitempty"`
}

// Where says in words where a declaration was made, for a refusal.
func (s Source) Where() string {
	if s.Kind == "file" {
		return fmt.Sprintf("the config file %s, at %s", s.Name, s.Key)
	}
	return "the startup configuration"
}

// Declared is an assignment made in the startup configuration. It overrides a
// stored one for the same function, and cannot be changed through the API
// while it is declared.
type Declared struct {
	Function api.AIFunction
	Adapter  string
	Model    string
	Source   Source
}

// Store is where assignments made through the API are kept.
type Store interface {
	List(ctx context.Context) ([]state.AIAssignment, error)
	Put(ctx context.Context, a state.AIAssignment) error
	Delete(ctx context.Context, function string) error
}

// Assignments is the service over AI function assignments.
type Assignments struct {
	Store    Store
	Registry *api.Registry
	Declared []Declared

	// Name returns an adapter's display name, for refusals: "the Anthropic
	// adapter (ai_anthropic)" is clearer than the ID alone. Nil uses the ID.
	Name func(ref string) string
}

// Function is one AI function as the API reports it.
type Function struct {
	Function api.AIFunction `json:"function"`
	Title    string         `json:"title"`

	// AdapterID is empty when nothing is assigned.
	AdapterID string `json:"adapter_id,omitempty"`

	// Model is the assignment's model, empty for the adapter's own.
	// EffectiveModel is what runs: this, else the adapter's default.
	Model          string `json:"model,omitempty"`
	EffectiveModel string `json:"effective_model,omitempty"`

	// On is whether a call would reach an adapter now. Off says why.
	On  bool   `json:"on"`
	Off string `json:"off,omitempty"`

	Source *Source `json:"source,omitempty"`

	// Overridden is a stored assignment the startup configuration replaces.
	// Remove the declaration and restart, and it applies again.
	Overridden *state.AIAssignment `json:"overridden,omitempty"`
}

// Load reads stored assignments, lays declared ones over them, and gives the
// result to the registry. Called at startup and after every change.
func (s *Assignments) Load(ctx context.Context) error {
	stored, err := s.Store.List(ctx)
	if err != nil {
		return err
	}
	byFn := map[api.AIFunction]api.AIAssignment{}
	for _, a := range stored {
		byFn[api.AIFunction(a.Function)] = api.AIAssignment{
			Function: api.AIFunction(a.Function), AdapterRef: a.AdapterID, Model: a.Model,
		}
	}
	for _, d := range s.Declared {
		byFn[d.Function] = api.AIAssignment{Function: d.Function, AdapterRef: d.Adapter, Model: d.Model}
	}
	out := make([]api.AIAssignment, 0, len(byFn))
	for _, a := range byFn {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Function < out[j].Function })
	s.Registry.SetAIAssignments(out)
	return nil
}

// List reports every assignable function, assigned or not.
func (s *Assignments) List(ctx context.Context) ([]Function, error) {
	stored, err := s.Store.List(ctx)
	if err != nil {
		return nil, err
	}
	storedBy := map[string]state.AIAssignment{}
	for _, a := range stored {
		storedBy[a.Function] = a
	}

	out := make([]Function, 0, len(api.AIFunctions()))
	for _, fn := range api.AIFunctions() {
		f := Function{Function: fn, Title: fn.Title()}
		row, hasRow := storedBy[string(fn)]
		if d, ok := s.declared(fn); ok {
			src := d.Source
			f.AdapterID, f.Model, f.Source = d.Adapter, d.Model, &src
			if hasRow && (row.AdapterID != d.Adapter || row.Model != d.Model) {
				r := row
				f.Overridden = &r
			}
		} else if hasRow {
			f.AdapterID, f.Model = row.AdapterID, row.Model
			f.Source = &Source{Kind: "database"}
		}
		f.On, f.Off, f.EffectiveModel = s.status(ctx, fn, f.AdapterID, f.Model)
		out = append(out, f)
	}
	return out, nil
}

// status says whether fn would reach adapter ref now, and on which model.
func (s *Assignments) status(ctx context.Context, fn api.AIFunction, ref, model string) (bool, string, string) {
	if ref == "" {
		return false, fmt.Sprintf("%s is not assigned to an AI adapter.", fn.Title()), ""
	}
	ai, ok := s.Registry.AI(ref)
	if !ok {
		return false, fmt.Sprintf("%s is assigned to %s, which is not running. It may have failed to start, "+
			"or been added since Pando started.", fn.Title(), s.name(ref)), ""
	}
	caps, err := ai.Capabilities(ctx)
	if err != nil {
		return false, fmt.Sprintf("Pando could not reach %s.", s.name(ref)), ""
	}
	if !caps.Does(fn) {
		return false, fmt.Sprintf("%s is assigned to %s, which does not perform it.", fn.Title(), s.name(ref)), ""
	}
	if model == "" || !caps.ChoosesModel {
		model = caps.Model
	}
	return true, "", model
}

// Assign gives fn to the adapter ref, on model (empty for the adapter's own).
func (s *Assignments) Assign(ctx context.Context, fn api.AIFunction, ref, model, by string) (Function, error) {
	if !api.IsAssignable(fn) {
		names := make([]string, 0, len(api.AIFunctions()))
		for _, f := range api.AIFunctions() {
			names = append(names, string(f))
		}
		return Function{}, errs.Newf(errs.ValidInvalid, "Pando has no AI function called %q.", fn).
			WithRemedy("Use one of: " + strings.Join(names, ", ") + ".")
	}
	if d, ok := s.declared(fn); ok {
		return Function{}, s.fixed(d)
	}
	ref = strings.TrimSpace(ref)
	model = strings.TrimSpace(model)
	if ref == "" {
		return Function{}, errs.Newf(errs.ValidInvalid, "Assigning %s needs an adapter.", strings.ToLower(fn.Title())).
			WithRemedy(`Send the AI adapter's ID, for example {"adapter_id": "ai_anthropic"}. GET /api/v1/adapters lists them.`)
	}

	ai, ok := s.Registry.AI(ref)
	if !ok {
		if _, exists := s.Registry.Get(ref); exists {
			return Function{}, errs.Newf(errs.ValidInvalid, "%s is not an AI adapter, so it cannot perform %s.",
				ref, strings.ToLower(fn.Title()))
		}
		return Function{}, errs.Newf(errs.StateInvalid,
			"There is no AI adapter %q running on this installation, so it cannot be assigned %s.",
			ref, strings.ToLower(fn.Title())).
			WithRemedy("If you added it since Pando started, restart Pando (POST /api/v1/restart, or pando restart) " +
				"and assign it again. GET /api/v1/adapters lists the adapters and whether each is running.")
	}
	caps, err := ai.Capabilities(ctx)
	if err != nil {
		return Function{}, errs.Newf(errs.AdapterUnavailable, "Pando could not ask %s what it can do.", s.name(ref)).
			WithRemedy("Check the adapter is reachable, then try again.")
	}
	if !caps.Does(fn) {
		does := make([]string, 0, len(caps.Functions))
		for _, f := range caps.Functions {
			does = append(does, strings.ToLower(f.Title()))
		}
		msg := fmt.Sprintf("%s does not perform %s, so it cannot be assigned it.", capitalize(s.name(ref)), strings.ToLower(fn.Title()))
		if len(does) > 0 {
			msg += " It performs: " + strings.Join(does, ", ") + "."
		}
		return Function{}, errs.New(errs.ValidInvalid, msg)
	}
	if model != "" {
		if !caps.ChoosesModel {
			return Function{}, errs.Newf(errs.ValidInvalid,
				"%s runs one model, %s, and cannot run %s for %s.",
				capitalize(s.name(ref)), caps.Model, model, strings.ToLower(fn.Title())).
				WithRemedy("Leave the model empty to use the adapter's own.")
		}
		if len(caps.Models) > 0 && !contains(caps.Models, model) {
			return Function{}, errs.Newf(errs.ValidInvalid, "%s cannot run %s.", capitalize(s.name(ref)), model).
				WithRemedy("Use one of: " + strings.Join(caps.Models, ", ") + ", or leave the model empty to use the adapter's own.")
		}
	}

	err = s.Store.Put(ctx, state.AIAssignment{Function: string(fn), AdapterID: ref, Model: model, UpdatedBy: by})
	var taken state.ErrAssigned
	if errors.As(err, &taken) {
		title := fn.Title()
		return Function{}, errs.Newf(errs.StateAIFunctionAssigned,
			"%s is already handled by %s. Each AI function is handled by one adapter at a time. "+
				"To move %s to %s, remove it from %s first.",
			title, s.name(taken.Held.AdapterID), strings.ToLower(title), s.name(ref), s.name(taken.Held.AdapterID)).
			WithRemedy(fmt.Sprintf("DELETE /api/v1/ai/functions/%s, or pando ai unassign %s, then assign it again.", fn, fn))
	}
	if err != nil {
		return Function{}, err
	}
	if err := s.Load(ctx); err != nil {
		return Function{}, err
	}
	return s.one(ctx, fn)
}

// Unassign turns fn off.
func (s *Assignments) Unassign(ctx context.Context, fn api.AIFunction) (Function, error) {
	if !api.IsAssignable(fn) {
		return Function{}, errs.Newf(errs.ValidInvalid, "Pando has no AI function called %q.", fn)
	}
	if d, ok := s.declared(fn); ok {
		return Function{}, s.fixed(d)
	}
	if err := s.Store.Delete(ctx, string(fn)); err != nil {
		return Function{}, err
	}
	if err := s.Load(ctx); err != nil {
		return Function{}, err
	}
	return s.one(ctx, fn)
}

func (s *Assignments) one(ctx context.Context, fn api.AIFunction) (Function, error) {
	all, err := s.List(ctx)
	if err != nil {
		return Function{}, err
	}
	for _, f := range all {
		if f.Function == fn {
			return f, nil
		}
	}
	return Function{}, errs.New(errs.Internal, "The AI function disappeared while it was being read.")
}

func (s *Assignments) declared(fn api.AIFunction) (Declared, bool) {
	for _, d := range s.Declared {
		if d.Function == fn {
			return d, true
		}
	}
	return Declared{}, false
}

// fixed is the refusal for a function the startup configuration assigns.
func (s *Assignments) fixed(d Declared) error {
	return errs.Newf(errs.StateSetAtStartup,
		"%s is assigned to %s in %s, so it cannot be changed here.", d.Function.Title(), d.Adapter, d.Source.Where()).
		WithRemedy("Change it there and restart Pando, or remove it there to manage it here.")
}

func (s *Assignments) name(ref string) string {
	if s.Name != nil {
		if n := s.Name(ref); n != "" && n != ref {
			return fmt.Sprintf("the %s adapter (%s)", n, ref)
		}
	}
	return "the adapter " + ref
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
