package assist

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/state"
	"github.com/bemeek-io/pando/internal/errs"
)

// fakeStore keeps assignments in memory and, like the database, refuses a
// function another adapter already holds.
type fakeStore struct {
	rows map[string]state.AIAssignment

	listErr, putErr, deleteErr error
	// failListAfterPut fails every List once a Put has succeeded.
	failListAfterPut bool
	put              bool
	// failListCall fails the nth List after a Put, counting from 1.
	failListCall  int
	listsAfterPut int
}

func newStore(rows ...state.AIAssignment) *fakeStore {
	s := &fakeStore{rows: map[string]state.AIAssignment{}}
	for _, r := range rows {
		s.rows[r.Function] = r
	}
	return s
}

func (s *fakeStore) List(context.Context) ([]state.AIAssignment, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	if s.put {
		s.listsAfterPut++
		if s.failListAfterPut || s.listsAfterPut == s.failListCall {
			return nil, errBoom
		}
	}
	out := make([]state.AIAssignment, 0, len(s.rows))
	for _, r := range s.rows {
		out = append(out, r)
	}
	return out, nil
}

func (s *fakeStore) Put(_ context.Context, a state.AIAssignment) error {
	if s.putErr != nil {
		return s.putErr
	}
	if held, ok := s.rows[a.Function]; ok && held.AdapterID != a.AdapterID {
		return state.ErrAssigned{Held: held}
	}
	s.rows[a.Function] = a
	s.put = true
	return nil
}

func (s *fakeStore) Delete(_ context.Context, function string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.rows, function)
	return nil
}

// notAI is a registered adapter of another category.
type notAI struct{ api.NotifyAdapter }

func (notAI) Category() api.Category { return api.CategoryNotify }

const fileSource = "/etc/pando/pando.yaml"

// newAssignments registers ai as ai_a (when non-nil), a choosing adapter as
// ai_b, a non-AI adapter as notify_log, and uses store.
func newAssignments(t *testing.T, store *fakeStore, ai *fakeAI) (*Assignments, *fakeAI) {
	t.Helper()
	reg := api.NewRegistry()
	if ai != nil {
		require.NoError(t, reg.Register("ai_a", ai))
	}
	b := &fakeAI{caps: api.AICapabilities{
		Functions: assistFunctions, Model: "b-default", ChoosesModel: true, Models: []string{"b-small", "b-large"},
	}}
	require.NoError(t, reg.Register("ai_b", b))
	require.NoError(t, reg.Register("notify_log", notAI{}))
	return &Assignments{Store: store, Registry: reg}, b
}

func byFunction(t *testing.T, fs []Function, fn api.AIFunction) Function {
	t.Helper()
	for _, f := range fs {
		if f.Function == fn {
			return f
		}
	}
	t.Fatalf("%s not listed", fn)
	return Function{}
}

// ---------------------------------------------------------------------------
// Load and List

// TestR271_DeclaredAssignmentOverridesStored asserts Load gives the registry
// the startup configuration's assignment where both say something, and the
// stored one elsewhere.
func TestR271_DeclaredAssignmentOverridesStored(t *testing.T) {
	store := newStore(
		state.AIAssignment{Function: "draft_access", AdapterID: "ai_a"},
		state.AIAssignment{Function: "search_audit", AdapterID: "ai_a", Model: "m1"},
	)
	s, _ := newAssignments(t, store, &fakeAI{caps: doesAll()})
	s.Declared = []Declared{{Function: api.AIFunctionDraftAccess, Adapter: "ai_b", Model: "b-small"}}

	require.NoError(t, s.Load(context.Background()))
	assert.Equal(t, []api.AIAssignment{
		{Function: api.AIFunctionDraftAccess, AdapterRef: "ai_b", Model: "b-small"},
		{Function: api.AIFunctionSearchAudit, AdapterRef: "ai_a", Model: "m1"},
	}, s.Registry.AIAssignments())
}

// TestLoadPropagatesStoreError asserts a store that cannot be read leaves the
// registry as it was.
func TestLoadPropagatesStoreError(t *testing.T) {
	store := newStore()
	store.listErr = errBoom
	s, _ := newAssignments(t, store, nil)
	s.Registry.SetAIAssignments([]api.AIAssignment{{Function: api.AIFunctionDraftAccess, AdapterRef: "ai_b"}})

	require.ErrorIs(t, s.Load(context.Background()), errBoom)
	assert.Len(t, s.Registry.AIAssignments(), 1)
}

// TestR259_ListReportsEveryFunctionAndWhyItIsOff asserts every assignable
// function is listed, with where its assignment came from and, when it would
// not reach an adapter, why.
func TestR259_ListReportsEveryFunctionAndWhyItIsOff(t *testing.T) {
	store := newStore(
		state.AIAssignment{Function: "repair_plan", AdapterID: "ai_a"},        // does not perform it
		state.AIAssignment{Function: "revise_plan", AdapterID: "ai_gone"},     // not running
		state.AIAssignment{Function: "draft_access", AdapterID: "ai_a"},       // on
		state.AIAssignment{Function: "draft_policy", AdapterID: "ai_b"},       // overridden below
		state.AIAssignment{Function: "search_audit", AdapterID: "ai_b"},       // declared the same
		state.AIAssignment{Function: "answer_reference", AdapterID: "ai_off"}, // capabilities fail
	)
	s, _ := newAssignments(t, store, &fakeAI{caps: doesAll()})
	require.NoError(t, s.Registry.Register("ai_off", &fakeAI{capsErr: errBoom}))
	src := Source{Kind: "file", Name: fileSource, Key: "ai.functions.draft_policy"}
	s.Declared = []Declared{
		{Function: api.AIFunctionDraftPolicy, Adapter: "ai_a", Source: src},
		{Function: api.AIFunctionSearchAudit, Adapter: "ai_b", Source: Source{Kind: "file", Name: fileSource}},
	}

	fs, err := s.List(context.Background())
	require.NoError(t, err)
	require.Len(t, fs, len(api.AIFunctions()))
	for i, fn := range api.AIFunctions() {
		assert.Equal(t, fn, fs[i].Function, "listed in AIFunctions order")
		assert.Equal(t, fn.Title(), fs[i].Title)
	}

	f := byFunction(t, fs, api.AIFunctionRepairPlan)
	assert.False(t, f.On)
	assert.Equal(t, "Plan repair is assigned to the adapter ai_a, which does not perform it.", f.Off)
	assert.Equal(t, &Source{Kind: "database"}, f.Source)

	f = byFunction(t, fs, api.AIFunctionAnswerQuestions)
	assert.False(t, f.On)
	assert.Equal(t, "Answering detection questions is not assigned to an AI adapter.", f.Off)
	assert.Nil(t, f.Source)
	assert.Empty(t, f.AdapterID)

	f = byFunction(t, fs, api.AIFunctionRevisePlan)
	assert.False(t, f.On)
	assert.Contains(t, f.Off, "Plan revision is assigned to the adapter ai_gone, which is not running.")

	f = byFunction(t, fs, api.AIFunctionAnswerReference)
	assert.False(t, f.On)
	assert.Equal(t, "Pando could not reach the adapter ai_off.", f.Off)
	assert.Empty(t, f.EffectiveModel)

	f = byFunction(t, fs, api.AIFunctionDraftAccess)
	assert.True(t, f.On)
	assert.Empty(t, f.Off)
	assert.Equal(t, "house-model", f.EffectiveModel, "no model assigned: the adapter's own")

	f = byFunction(t, fs, api.AIFunctionDraftPolicy)
	assert.Equal(t, "ai_a", f.AdapterID)
	assert.Equal(t, &src, f.Source)
	require.NotNil(t, f.Overridden, "the stored row the declaration replaces is reported")
	assert.Equal(t, "ai_b", f.Overridden.AdapterID)

	f = byFunction(t, fs, api.AIFunctionSearchAudit)
	assert.Nil(t, f.Overridden, "a stored row matching the declaration is not an override")
	assert.Equal(t, "file", f.Source.Kind)
}

// TestR259_EffectiveModelIsTheAssignedOneOnlyWhereItCanRun asserts the model
// reported as running is the assignment's on an adapter that chooses, and the
// adapter's own otherwise.
func TestR259_EffectiveModelIsTheAssignedOneOnlyWhereItCanRun(t *testing.T) {
	store := newStore(
		state.AIAssignment{Function: "draft_access", AdapterID: "ai_b", Model: "b-large"},
		state.AIAssignment{Function: "draft_policy", AdapterID: "ai_b"},
		state.AIAssignment{Function: "search_audit", AdapterID: "ai_a", Model: "ignored"},
	)
	s, _ := newAssignments(t, store, &fakeAI{caps: doesAll()})

	fs, err := s.List(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "b-large", byFunction(t, fs, api.AIFunctionDraftAccess).EffectiveModel)
	assert.Equal(t, "b-default", byFunction(t, fs, api.AIFunctionDraftPolicy).EffectiveModel)
	f := byFunction(t, fs, api.AIFunctionSearchAudit)
	assert.Equal(t, "ignored", f.Model)
	assert.Equal(t, "house-model", f.EffectiveModel)
}

// TestListPropagatesStoreError asserts a store that cannot be read is an
// error, not a list of unassigned functions.
func TestListPropagatesStoreError(t *testing.T) {
	store := newStore()
	store.listErr = errBoom
	s, _ := newAssignments(t, store, nil)
	_, err := s.List(context.Background())
	require.ErrorIs(t, err, errBoom)
}

// ---------------------------------------------------------------------------
// Assign

// TestR259_AssignStoresAndTurnsTheFunctionOn asserts an assignment is stored
// with who made it, reaches the registry, and is reported on.
func TestR259_AssignStoresAndTurnsTheFunctionOn(t *testing.T) {
	store := newStore()
	s, b := newAssignments(t, store, nil)

	f, err := s.Assign(context.Background(), api.AIFunctionSearchAudit, "  ai_b ", " b-small ", "usr_admin")
	require.NoError(t, err)
	assert.Equal(t, Function{
		Function: api.AIFunctionSearchAudit, Title: "Audit search", AdapterID: "ai_b",
		Model: "b-small", EffectiveModel: "b-small", On: true, Source: &Source{Kind: "database"},
	}, f)
	assert.Equal(t, state.AIAssignment{Function: "search_audit", AdapterID: "ai_b", Model: "b-small", UpdatedBy: "usr_admin"},
		store.rows["search_audit"])

	got, a, ok := s.Registry.AIFor(api.AIFunctionSearchAudit)
	require.True(t, ok)
	assert.Same(t, b, got)
	assert.Equal(t, "b-small", a.Model)

	// Changing only the model on the same adapter is allowed.
	f, err = s.Assign(context.Background(), api.AIFunctionSearchAudit, "ai_b", "", "usr_admin")
	require.NoError(t, err)
	assert.Equal(t, "b-default", f.EffectiveModel)
}

// TestAssignRefusesAnUnknownFunction asserts a function Pando does not have
// is refused, listing the ones it does.
func TestAssignRefusesAnUnknownFunction(t *testing.T) {
	s, _ := newAssignments(t, newStore(), nil)

	for _, fn := range []api.AIFunction{"write_poetry", api.AIFunctionReadReadme} {
		_, err := s.Assign(context.Background(), fn, "ai_b", "", "usr_admin")
		e := requireCode(t, err, errs.ValidInvalid)
		assert.Equal(t, `Pando has no AI function called "`+string(fn)+`".`, e.Message)
		assert.Equal(t, "Use one of: repair_plan, answer_questions, revise_plan, draft_access, draft_policy, search_audit, answer_reference.", e.Remedy)
	}
}

// TestR271_AssignRefusesADeclaredFunction asserts a function the startup
// configuration assigns cannot be reassigned through the API, and the refusal
// says where it was declared.
func TestR271_AssignRefusesADeclaredFunction(t *testing.T) {
	store := newStore()
	s, _ := newAssignments(t, store, nil)
	s.Declared = []Declared{{
		Function: api.AIFunctionDraftPolicy, Adapter: "ai_a",
		Source: Source{Kind: "file", Name: fileSource, Key: "ai.functions.draft_policy"},
	}}

	_, err := s.Assign(context.Background(), api.AIFunctionDraftPolicy, "ai_b", "", "usr_admin")
	e := requireCode(t, err, errs.StateSetAtStartup)
	assert.Equal(t, "Policy drafting is assigned to ai_a in the config file "+fileSource+", at ai.functions.draft_policy, so it cannot be changed here.", e.Message)
	assert.Equal(t, "Change it there and restart Pando, or remove it there to manage it here.", e.Remedy)
	assert.Empty(t, store.rows)
}

// TestSourceWhereWithoutAFile asserts a declaration from somewhere other than
// a file is described generally.
func TestSourceWhereWithoutAFile(t *testing.T) {
	assert.Equal(t, "the startup configuration", Source{Kind: "env"}.Where())
}

// TestAssignRefusesWhatCannotRunTheFunction asserts each way an adapter can be
// wrong for a function is refused with a message naming it.
func TestAssignRefusesWhatCannotRunTheFunction(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name, ref, model string
		code             errs.Code
		message, remedy  string
	}{
		{
			name: "no adapter", ref: "  ", code: errs.ValidInvalid,
			message: "Assigning audit search needs an adapter.",
			remedy:  `Send the AI adapter's ID, for example {"adapter_id": "ai_anthropic"}. GET /api/v1/adapters lists them.`,
		},
		{
			name: "not an AI adapter", ref: "notify_log", code: errs.ValidInvalid,
			message: "notify_log is not an AI adapter, so it cannot perform audit search.",
		},
		{
			name: "not running", ref: "ai_nowhere", code: errs.StateInvalid,
			message: `There is no AI adapter "ai_nowhere" running on this installation, so it cannot be assigned audit search.`,
		},
		{
			name: "capabilities fail", ref: "ai_off", code: errs.AdapterUnavailable,
			message: "Pando could not ask the adapter ai_off what it can do.",
			remedy:  "Check the adapter is reachable, then try again.",
		},
		{
			name: "does not perform it", ref: "ai_a", code: errs.ValidInvalid,
			message: "The adapter ai_a does not perform audit search, so it cannot be assigned it. It performs: plan repair, reference help.",
		},
		{
			name: "performs nothing", ref: "ai_none", code: errs.ValidInvalid,
			message: "The adapter ai_none does not perform audit search, so it cannot be assigned it.",
		},
		{
			name: "one model only", ref: "ai_one", model: "bigger", code: errs.ValidInvalid,
			message: "The adapter ai_one runs one model, one-model, and cannot run bigger for audit search.",
			remedy:  "Leave the model empty to use the adapter's own.",
		},
		{
			name: "model not offered", ref: "ai_b", model: "b-huge", code: errs.ValidInvalid,
			message: "The adapter ai_b cannot run b-huge.",
			remedy:  "Use one of: b-small, b-large, or leave the model empty to use the adapter's own.",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store := newStore()
			s, _ := newAssignments(t, store, &fakeAI{caps: api.AICapabilities{
				Functions: []api.AIFunction{api.AIFunctionRepairPlan, api.AIFunctionAnswerReference},
			}})
			require.NoError(t, s.Registry.Register("ai_off", &fakeAI{capsErr: errBoom}))
			require.NoError(t, s.Registry.Register("ai_none", &fakeAI{}))
			require.NoError(t, s.Registry.Register("ai_one", &fakeAI{caps: api.AICapabilities{Functions: assistFunctions, Model: "one-model"}}))

			_, err := s.Assign(ctx, api.AIFunctionSearchAudit, c.ref, c.model, "usr_admin")
			e := requireCode(t, err, c.code)
			assert.Equal(t, c.message, e.Message)
			if c.remedy != "" {
				assert.Equal(t, c.remedy, e.Remedy)
			}
			if c.name == "not running" {
				assert.Contains(t, e.Remedy, "restart Pando")
			}
			assert.Empty(t, store.rows, "nothing is stored")
		})
	}
}

// TestAssignAnyModelWhenTheAdapterListsNone asserts an adapter that chooses
// models without listing them accepts any name.
func TestAssignAnyModelWhenTheAdapterListsNone(t *testing.T) {
	s, _ := newAssignments(t, newStore(), &fakeAI{caps: api.AICapabilities{Functions: assistFunctions, ChoosesModel: true, Model: "a-default"}})
	f, err := s.Assign(context.Background(), api.AIFunctionDraftAccess, "ai_a", "a-anything", "usr_admin")
	require.NoError(t, err)
	assert.Equal(t, "a-anything", f.EffectiveModel)
}

// TestR259_AssignRefusesAFunctionAnotherAdapterHolds asserts one adapter per
// function: moving it names the holder and says how to free it.
func TestR259_AssignRefusesAFunctionAnotherAdapterHolds(t *testing.T) {
	store := newStore(state.AIAssignment{Function: "search_audit", AdapterID: "ai_a"})
	s, _ := newAssignments(t, store, &fakeAI{caps: doesAll()})
	s.Name = func(ref string) string {
		return map[string]string{"ai_a": "Anthropic", "ai_b": "OpenAI"}[ref]
	}

	_, err := s.Assign(context.Background(), api.AIFunctionSearchAudit, "ai_b", "", "usr_admin")
	e := requireCode(t, err, errs.StateAIFunctionAssigned)
	assert.Equal(t, "Audit search is already handled by the Anthropic adapter (ai_a). Each AI function is handled by one adapter at a time. "+
		"To move audit search to the OpenAI adapter (ai_b), remove it from the Anthropic adapter (ai_a) first.", e.Message)
	assert.Equal(t, "DELETE /api/v1/ai/functions/search_audit, or pando ai unassign search_audit, then assign it again.", e.Remedy)
	assert.Equal(t, "ai_a", store.rows["search_audit"].AdapterID)
}

// TestAssignPropagatesStoreErrors asserts a failed write, or a failed reread
// after it, is returned.
func TestAssignPropagatesStoreErrors(t *testing.T) {
	store := newStore()
	store.putErr = errBoom
	s, _ := newAssignments(t, store, nil)
	_, err := s.Assign(context.Background(), api.AIFunctionDraftAccess, "ai_b", "", "usr_admin")
	require.ErrorIs(t, err, errBoom)

	store = newStore()
	store.failListAfterPut = true
	s, _ = newAssignments(t, store, nil)
	_, err = s.Assign(context.Background(), api.AIFunctionDraftAccess, "ai_b", "", "usr_admin")
	require.ErrorIs(t, err, errBoom)

	// The registry reloads, and reading the result back fails.
	store = newStore()
	store.failListCall = 2
	s, _ = newAssignments(t, store, nil)
	_, err = s.Assign(context.Background(), api.AIFunctionDraftAccess, "ai_b", "", "usr_admin")
	require.ErrorIs(t, err, errBoom)
}

// TestAdapterNameInRefusals asserts refusals name an adapter by display name
// and ID when one is known, and by ID alone otherwise.
func TestAdapterNameInRefusals(t *testing.T) {
	s := &Assignments{}
	assert.Equal(t, "the adapter ai_a", s.name("ai_a"))

	s.Name = func(ref string) string {
		switch ref {
		case "ai_a":
			return "Anthropic"
		case "ai_same":
			return "ai_same"
		}
		return ""
	}
	assert.Equal(t, "the Anthropic adapter (ai_a)", s.name("ai_a"))
	assert.Equal(t, "the adapter ai_same", s.name("ai_same"), "a name that is only the ID adds nothing")
	assert.Equal(t, "the adapter ai_x", s.name("ai_x"))

	// And it reaches the list: "The Anthropic adapter" starts a sentence.
	reg := api.NewRegistry()
	require.NoError(t, reg.Register("ai_a", &fakeAI{caps: api.AICapabilities{}}))
	s.Registry, s.Store = reg, newStore()
	_, err := s.Assign(context.Background(), api.AIFunctionDraftAccess, "ai_a", "", "usr_admin")
	e := requireCode(t, err, errs.ValidInvalid)
	assert.Equal(t, "The Anthropic adapter (ai_a) does not perform access drafting, so it cannot be assigned it.", e.Message)
}

// ---------------------------------------------------------------------------
// Unassign

// TestR259_UnassignTurnsTheFunctionOff asserts removing an assignment deletes
// the stored row and takes it out of the registry.
func TestR259_UnassignTurnsTheFunctionOff(t *testing.T) {
	store := newStore(state.AIAssignment{Function: "draft_access", AdapterID: "ai_b"})
	s, _ := newAssignments(t, store, nil)
	require.NoError(t, s.Load(context.Background()))

	f, err := s.Unassign(context.Background(), api.AIFunctionDraftAccess)
	require.NoError(t, err)
	assert.False(t, f.On)
	assert.Equal(t, "Access drafting is not assigned to an AI adapter.", f.Off)
	assert.Empty(t, store.rows)
	_, _, ok := s.Registry.AIFor(api.AIFunctionDraftAccess)
	assert.False(t, ok)
}

// TestUnassignRefusals asserts an unknown function and a declared one are
// refused, and a failed delete or reread is returned.
func TestUnassignRefusals(t *testing.T) {
	ctx := context.Background()
	store := newStore(state.AIAssignment{Function: "draft_policy", AdapterID: "ai_b"})
	s, _ := newAssignments(t, store, nil)

	_, err := s.Unassign(ctx, "write_poetry")
	e := requireCode(t, err, errs.ValidInvalid)
	assert.Equal(t, `Pando has no AI function called "write_poetry".`, e.Message)

	s.Declared = []Declared{{Function: api.AIFunctionDraftPolicy, Adapter: "ai_b"}}
	_, err = s.Unassign(ctx, api.AIFunctionDraftPolicy)
	e = requireCode(t, err, errs.StateSetAtStartup)
	assert.Contains(t, e.Message, "in the startup configuration")
	assert.Len(t, store.rows, 1, "the stored row is kept")

	s.Declared = nil
	store.deleteErr = errBoom
	_, err = s.Unassign(ctx, api.AIFunctionDraftPolicy)
	require.ErrorIs(t, err, errBoom)

	store.deleteErr = nil
	store.listErr = errBoom
	_, err = s.Unassign(ctx, api.AIFunctionDraftPolicy)
	require.ErrorIs(t, err, errBoom)
}

// TestCapitalizeAndContains covers the two string helpers.
func TestCapitalizeAndContains(t *testing.T) {
	assert.Equal(t, "", capitalize(""))
	assert.Equal(t, "The adapter", capitalize("the adapter"))
	assert.True(t, contains([]string{"a", "b"}, "b"))
	assert.False(t, contains([]string{"a", "b"}, "c"))
	assert.False(t, contains(nil, "a"))
}

// TestFunctionJSONOmitsWhatIsUnset asserts an unassigned function reads as
// off without empty adapter, model or source fields.
func TestFunctionJSONOmitsWhatIsUnset(t *testing.T) {
	raw, err := json.Marshal(Function{Function: api.AIFunctionDraftAccess, Title: "Access drafting", Off: "not assigned"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"function":"draft_access","title":"Access drafting","on":false,"off":"not assigned"}`, string(raw))
}
