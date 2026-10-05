package conflict

import (
	"errors"
	"strings"
	"testing"
)

func TestResolve_AddWhenUnrelated(t *testing.T) {
	e := NewEngine()
	existing := []Fact{{ID: "1", Content: "User prefers Go for backend services"}}
	d := e.Resolve("User is allergic to shellfish", existing)
	if d.Action != ActionAdd {
		t.Fatalf("unrelated fact: want add, got %s (sim=%.2f)", d.Action, d.Similarity)
	}
}

func TestResolve_UpdateOnContradiction_JobSwitch(t *testing.T) {
	// The interview scenario: same subject, changed value.
	e := NewEngine()
	existing := []Fact{{ID: "old", Content: "User works at Google as a backend engineer"}}
	d := e.Resolve("User works at OpenAI as a backend engineer", existing)
	if d.Action != ActionUpdate {
		t.Fatalf("job switch: want update, got %s (sim=%.2f)", d.Action, d.Similarity)
	}
	if d.TargetID != "old" {
		t.Fatalf("want target 'old', got %q", d.TargetID)
	}
}

// TestResolve_ShortAntonym_BoundaryOfHeuristic documents a known limitation:
// for very short facts where the changed word is most of the content, lexical
// overlap can't distinguish a contradiction ("love"→"hate") from an addition
// ("Python"→"Rust") — both share only "job"/"likes" and score ~0.33. The bare
// heuristic conservatively treats this as Add (never wrongly erase). Semantic
// resolution of this band is what the optional Resolver is for.
func TestResolve_ShortAntonym_BoundaryOfHeuristic(t *testing.T) {
	e := NewEngine()
	existing := []Fact{{ID: "x", Content: "I love my job"}}
	d := e.Resolve("I hate my job", existing)
	if d.Action != ActionAdd {
		t.Fatalf("bare heuristic on short antonym: want add (conservative), got %s (sim=%.2f)", d.Action, d.Similarity)
	}
}

// With a Resolver that understands semantics, the same band is resolvable. Here
// we lower the conflict threshold to route the short fact into the Resolver,
// proving the hook is the intended path for semantic contradictions.
func TestResolve_ShortAntonym_ResolvableViaResolver(t *testing.T) {
	stub := &stubResolver{decide: ActionUpdate, reason: "semantic contradiction"}
	e := NewEngine()
	e.ConflictThreshold = 0.3 // widen the band so the Resolver sees this case
	e.Resolver = stub
	existing := []Fact{{ID: "x", Content: "I love my job"}}
	d := e.Resolve("I hate my job", existing)
	if !stub.called || d.Action != ActionUpdate {
		t.Fatalf("with resolver: want update, got %s called=%v", d.Action, stub.called)
	}
}

func TestResolve_Duplicate(t *testing.T) {
	e := NewEngine()
	existing := []Fact{{ID: "d", Content: "User works at OpenAI as a backend engineer"}}
	d := e.Resolve("User works at OpenAI as a backend engineer", existing)
	if d.Action != ActionDuplicate {
		t.Fatalf("identical: want duplicate, got %s (sim=%.2f)", d.Action, d.Similarity)
	}
}

func TestResolve_EmptyCandidates(t *testing.T) {
	e := NewEngine()
	if d := e.Resolve("anything", nil); d.Action != ActionAdd {
		t.Fatalf("no candidates: want add, got %s", d.Action)
	}
}

func TestResolve_PicksMostSimilarTarget(t *testing.T) {
	e := NewEngine()
	existing := []Fact{
		{ID: "a", Content: "User lives in Berlin"},
		{ID: "b", Content: "User works at Google as a backend engineer"},
	}
	d := e.Resolve("User works at OpenAI as a backend engineer", existing)
	if d.Action != ActionUpdate || d.TargetID != "b" {
		t.Fatalf("want update of 'b', got %s target=%q", d.Action, d.TargetID)
	}
}

// stubResolver lets us assert the borderline-band hook is consulted, with a
// configurable decision.
type stubResolver struct {
	called bool
	decide Action
	reason string
}

func (s *stubResolver) Resolve(_ string, _ Fact) (Action, string, error) {
	s.called = true
	return s.decide, s.reason, nil
}

func TestResolve_ResolverConsultedInConflictBand(t *testing.T) {
	stub := &stubResolver{decide: ActionAdd, reason: "resolver override"}
	e := NewEngine()
	e.Resolver = stub
	existing := []Fact{{ID: "old", Content: "User works at Google as a backend engineer"}}
	d := e.Resolve("User works at OpenAI as a backend engineer", existing)
	if !stub.called {
		t.Fatal("resolver should be consulted in the conflict band")
	}
	if d.Action != ActionAdd || d.Reason != "resolver override" {
		t.Fatalf("resolver decision should win, got %s reason=%q", d.Action, d.Reason)
	}
}

func TestJaccard(t *testing.T) {
	a := tokenize("user works google")
	b := tokenize("user works openai")
	if got := jaccard(a, a); got != 1.0 {
		t.Fatalf("self jaccard want 1.0, got %.2f", got)
	}
	if got := jaccard(a, b); got <= 0 || got >= 1 {
		t.Fatalf("partial overlap should be in (0,1), got %.2f", got)
	}
}

type scriptedResolver struct {
	calls  []string
	decide func(candidate Fact) (Action, string, error)
}

func (s *scriptedResolver) Resolve(_ string, candidate Fact) (Action, string, error) {
	s.calls = append(s.calls, candidate.ID)
	return s.decide(candidate)
}

func sameSubject(subject string) func(Fact) (Action, string, error) {
	return func(c Fact) (Action, string, error) {
		if strings.Contains(c.Content, subject) {
			return ActionUpdate, "same subject", nil
		}
		return ActionAdd, "different subject", nil
	}
}

func crossSubjectFacts() []Fact {
	return []Fact{
		{ID: "other", Content: "Nadia Bellweather works at Harbor Partners"},
		{ID: "mine", Content: "Marisol Bellweather works at Cinder Labs"},
	}
}

const crossSubjectNew = "Marisol Bellweather works at Harbor Partners"

func TestResolve_DefaultConsultsOnlyBestCandidate(t *testing.T) {
	r := &scriptedResolver{decide: sameSubject("Marisol")}
	e := NewEngine()
	e.ConflictThreshold = 0.2
	e.Resolver = r
	d := e.Resolve(crossSubjectNew, crossSubjectFacts())
	if len(r.calls) != 1 || r.calls[0] != "other" {
		t.Fatalf("default must consult only the best-overlap candidate, got calls=%v", r.calls)
	}
	if d.Action != ActionAdd {
		t.Fatalf("legacy behavior: want add after the best candidate is rejected, got %s", d.Action)
	}
}

func TestResolve_MaxCandidatesWalksPastWrongSubject(t *testing.T) {
	r := &scriptedResolver{decide: sameSubject("Marisol")}
	e := NewEngine()
	e.ConflictThreshold = 0.2
	e.MaxCandidates = 3
	e.Resolver = r
	d := e.Resolve(crossSubjectNew, crossSubjectFacts())
	if d.Action != ActionUpdate || d.TargetID != "mine" {
		t.Fatalf("want update of 'mine', got %s target=%q", d.Action, d.TargetID)
	}
	if len(r.calls) != 2 || r.calls[0] != "other" || r.calls[1] != "mine" {
		t.Fatalf("candidates must be consulted in similarity order, got %v", r.calls)
	}
}

func TestResolve_MaxCandidatesAllRejectedIsAdd(t *testing.T) {
	r := &scriptedResolver{decide: func(Fact) (Action, string, error) { return ActionAdd, "no", nil }}
	e := NewEngine()
	e.ConflictThreshold = 0.2
	e.MaxCandidates = 3
	e.Resolver = r
	d := e.Resolve(crossSubjectNew, crossSubjectFacts())
	if d.Action != ActionAdd || d.TargetID != "" {
		t.Fatalf("want add with no target, got %s target=%q", d.Action, d.TargetID)
	}
	if len(r.calls) != 2 {
		t.Fatalf("both candidates in the band must be consulted, got %v", r.calls)
	}
}

func TestResolve_MaxCandidatesBoundsResolverCalls(t *testing.T) {
	r := &scriptedResolver{decide: func(Fact) (Action, string, error) { return ActionAdd, "no", nil }}
	e := NewEngine()
	e.ConflictThreshold = 0.2
	e.MaxCandidates = 2
	e.Resolver = r
	facts := []Fact{
		{ID: "1", Content: "Marisol Bellweather works at Harbor Partners"},
		{ID: "2", Content: "Nadia Bellweather works at Harbor Partners"},
		{ID: "3", Content: "Idris Okonkwo works at Harbor Partners"},
		{ID: "4", Content: "Ines Ferreira works at Harbor Partners"},
	}
	e.Resolve("Kenji Osgood works at Harbor Partners", facts)
	if len(r.calls) != 2 {
		t.Fatalf("resolver calls must be capped at MaxCandidates=2, got %v", r.calls)
	}
}

func TestResolve_MaxCandidatesSkipsCandidatesBelowThreshold(t *testing.T) {
	r := &scriptedResolver{decide: func(Fact) (Action, string, error) { return ActionAdd, "no", nil }}
	e := NewEngine()
	e.MaxCandidates = 5
	e.Resolver = r
	facts := []Fact{
		{ID: "close", Content: "User works at Google as a backend engineer"},
		{ID: "far", Content: "User is allergic to shellfish"},
	}
	e.Resolve("User works at OpenAI as a backend engineer", facts)
	if len(r.calls) != 1 || r.calls[0] != "close" {
		t.Fatalf("only candidates at or above ConflictThreshold may be consulted, got %v", r.calls)
	}
}

func TestResolve_ResolverErrorFallsBackToHeuristicOnBestCandidate(t *testing.T) {
	r := &scriptedResolver{decide: func(Fact) (Action, string, error) { return ActionAdd, "", errors.New("down") }}
	e := NewEngine()
	e.MaxCandidates = 3
	e.Resolver = r
	facts := []Fact{{ID: "old", Content: "User works at Google as a backend engineer"}}
	d := e.Resolve("User works at OpenAI as a backend engineer", facts)
	if d.Action != ActionUpdate || d.TargetID != "old" {
		t.Fatalf("resolver error must keep the legacy heuristic update, got %s target=%q", d.Action, d.TargetID)
	}
	if len(r.calls) != 1 {
		t.Fatalf("an error must stop the walk, got calls=%v", r.calls)
	}
}

func TestResolve_DuplicateShortCircuitsBeforeResolver(t *testing.T) {
	r := &scriptedResolver{decide: func(Fact) (Action, string, error) { return ActionUpdate, "", nil }}
	e := NewEngine()
	e.MaxCandidates = 3
	e.Resolver = r
	facts := []Fact{{ID: "d", Content: "User works at OpenAI as a backend engineer"}}
	d := e.Resolve("User works at OpenAI as a backend engineer", facts)
	if d.Action != ActionDuplicate || len(r.calls) != 0 {
		t.Fatalf("duplicate must not consult the resolver, got %s calls=%v", d.Action, r.calls)
	}
}

func TestResolve_TieOrderIsStableByInputOrder(t *testing.T) {
	r := &scriptedResolver{decide: func(Fact) (Action, string, error) { return ActionAdd, "no", nil }}
	e := NewEngine()
	e.ConflictThreshold = 0.2
	e.MaxCandidates = 3
	e.Resolver = r
	facts := []Fact{
		{ID: "first", Content: "Idris Okonkwo works at Harbor Partners"},
		{ID: "second", Content: "Ines Ferreira works at Harbor Partners"},
	}
	e.Resolve("Kenji Osgood works at Harbor Partners", facts)
	if len(r.calls) != 2 || r.calls[0] != "first" || r.calls[1] != "second" {
		t.Fatalf("equal similarity must keep input order, got %v", r.calls)
	}
}

func TestResolve_ResolverErrorAfterRejectionKeepsBothFacts(t *testing.T) {
	r := &scriptedResolver{decide: func(c Fact) (Action, string, error) {
		if c.ID == "other" {
			return ActionAdd, "different subject", nil
		}
		return ActionAdd, "", errors.New("down")
	}}
	e := NewEngine()
	e.ConflictThreshold = 0.2
	e.MaxCandidates = 3
	e.Resolver = r
	d := e.Resolve(crossSubjectNew, crossSubjectFacts())
	if d.Action != ActionAdd || d.TargetID != "" {
		t.Fatalf("an error after a rejection must not supersede the rejected candidate, got %s target=%q", d.Action, d.TargetID)
	}
}

type scriptedMulti struct {
	got    [][]string
	decide func(newContent string, candidates []Fact) (Action, int, string, error)
}

func (s *scriptedMulti) Resolve(string, Fact) (Action, string, error) {
	return ActionAdd, "single path must not be used", errors.New("single path used")
}

func (s *scriptedMulti) ResolveAmong(newContent string, candidates []Fact) (Action, int, string, error) {
	ids := make([]string, len(candidates))
	for i, c := range candidates {
		ids[i] = c.ID
	}
	s.got = append(s.got, ids)
	return s.decide(newContent, candidates)
}

func pickSubject(subject string) func(string, []Fact) (Action, int, string, error) {
	return func(_ string, cs []Fact) (Action, int, string, error) {
		for i, c := range cs {
			if strings.Contains(c.Content, subject) {
				return ActionUpdate, i, "same subject", nil
			}
		}
		return ActionAdd, -1, "none", nil
	}
}

func manyFacts() []Fact {
	return []Fact{
		{ID: "a", Content: "Nadia Bellweather works at Harbor Partners"},
		{ID: "b", Content: "Idris Okonkwo works at Harbor Partners"},
		{ID: "mine", Content: "Marisol Bellweather works at Cinder Labs"},
		{ID: "c", Content: "Ines Ferreira works at Harbor Partners"},
	}
}

func TestMulti_OneCallPicksTheRightCandidate(t *testing.T) {
	m := &scriptedMulti{decide: pickSubject("Marisol")}
	e := NewEngine()
	e.ConflictThreshold = 0.1
	e.MaxCandidates = 4
	e.Resolver = m
	d := e.Resolve(crossSubjectNew, manyFacts())
	if len(m.got) != 1 {
		t.Fatalf("want exactly one batched call, got %d", len(m.got))
	}
	if d.Action != ActionUpdate || d.TargetID != "mine" {
		t.Fatalf("want update of 'mine', got %s target=%q", d.Action, d.TargetID)
	}
}

func TestMulti_CandidatesAreRankedAndBounded(t *testing.T) {
	m := &scriptedMulti{decide: func(string, []Fact) (Action, int, string, error) { return ActionAdd, -1, "no", nil }}
	e := NewEngine()
	e.ConflictThreshold = 0.1
	e.MaxCandidates = 2
	e.Resolver = m
	e.Resolve(crossSubjectNew, manyFacts())
	if len(m.got) != 1 || len(m.got[0]) != 2 {
		t.Fatalf("want one call with 2 candidates, got %v", m.got)
	}
}

func TestMulti_NoCandidateAboveThresholdSkipsTheCall(t *testing.T) {
	m := &scriptedMulti{decide: pickSubject("x")}
	e := NewEngine()
	e.MaxCandidates = 3
	e.Resolver = m
	d := e.Resolve("User is allergic to shellfish", []Fact{{ID: "1", Content: "User prefers Go for backend"}})
	if d.Action != ActionAdd || len(m.got) != 0 {
		t.Fatalf("want add without a call, got %s calls=%d", d.Action, len(m.got))
	}
}

func TestMulti_ErrorKeepsBothFactsNeverSupersedes(t *testing.T) {
	m := &scriptedMulti{decide: func(string, []Fact) (Action, int, string, error) {
		return ActionAdd, -1, "", errors.New("down")
	}}
	e := NewEngine()
	e.ConflictThreshold = 0.1
	e.MaxCandidates = 4
	e.Resolver = m
	d := e.Resolve(crossSubjectNew, manyFacts())
	if d.Action != ActionAdd || d.TargetID != "" {
		t.Fatalf("a failed batch call must add and supersede nothing, got %s target=%q", d.Action, d.TargetID)
	}
}

func TestMulti_OutOfRangeIndexIsTreatedAsAnError(t *testing.T) {
	for _, idx := range []int{-2, 99} {
		m := &scriptedMulti{decide: func(string, []Fact) (Action, int, string, error) { return ActionUpdate, idx, "bad", nil }}
		e := NewEngine()
		e.ConflictThreshold = 0.1
		e.MaxCandidates = 4
		e.Resolver = m
		d := e.Resolve(crossSubjectNew, manyFacts())
		if d.Action != ActionAdd || d.TargetID != "" {
			t.Fatalf("index %d must not supersede anything, got %s target=%q", idx, d.Action, d.TargetID)
		}
	}
}

func TestMulti_UpdateWithoutTargetIsRejected(t *testing.T) {
	m := &scriptedMulti{decide: func(string, []Fact) (Action, int, string, error) { return ActionUpdate, -1, "no target", nil }}
	e := NewEngine()
	e.ConflictThreshold = 0.1
	e.MaxCandidates = 4
	e.Resolver = m
	d := e.Resolve(crossSubjectNew, manyFacts())
	if d.Action != ActionAdd || d.TargetID != "" {
		t.Fatalf("update with index -1 must be treated as add, got %s target=%q", d.Action, d.TargetID)
	}
}

func TestMulti_DuplicateIsHonored(t *testing.T) {
	m := &scriptedMulti{decide: func(_ string, cs []Fact) (Action, int, string, error) { return ActionDuplicate, 0, "same", nil }}
	e := NewEngine()
	e.ConflictThreshold = 0.1
	e.MaxCandidates = 4
	e.Resolver = m
	d := e.Resolve(crossSubjectNew, manyFacts())
	if d.Action != ActionDuplicate || d.TargetID == "" {
		t.Fatalf("want duplicate with a target, got %s target=%q", d.Action, d.TargetID)
	}
}

func TestMulti_NotUsedWhenMaxCandidatesIsOne(t *testing.T) {
	r := &scriptedMulti{decide: pickSubject("Marisol")}
	e := NewEngine()
	e.ConflictThreshold = 0.1
	e.Resolver = r
	e.Resolve(crossSubjectNew, manyFacts())
	if len(r.got) != 0 {
		t.Fatalf("with MaxCandidates<=1 the single-candidate path is used, got batched calls %v", r.got)
	}
}
