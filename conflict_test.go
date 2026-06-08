package conflict

import "testing"

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
