// Package conflict ("conflict-lens") decides what to do when a new fact arrives
// that may contradict, duplicate, or extend what is already known.
//
// This is the piece that turns a store into a memory: vector/keyword recall
// measures *similarity*, not *truth*. "I love my job" and "I quit" both mention
// the job and retrieve together, and a naive agent hallucinates a synthesis.
// conflict-lens classifies the relationship so the caller can supersede the old
// fact (keeping it as history) instead of accumulating contradictions.
//
// The package is deliberately free of any memkit dependency so it can be
// extracted and imported standalone.
package conflict

import (
	"sort"
	"strings"
)

// Action is the recommended disposition for a new fact.
type Action int

const (
	// ActionAdd: the new fact is unrelated to existing ones — store it.
	ActionAdd Action = iota
	// ActionUpdate: the new fact contradicts/refines Target — supersede Target.
	ActionUpdate
	// ActionDuplicate: the new fact restates Target — skip (or just refresh).
	ActionDuplicate
)

func (a Action) String() string {
	switch a {
	case ActionUpdate:
		return "update"
	case ActionDuplicate:
		return "duplicate"
	default:
		return "add"
	}
}

// Fact is the minimal shape conflict-lens needs. Callers adapt their own model
// (e.g. memkit's store.Memory) into this.
type Fact struct {
	ID      string
	Content string
}

// Decision is the outcome of Resolve.
type Decision struct {
	Action     Action  `json:"action"`
	TargetID   string  `json:"target_id,omitempty"` // existing fact to supersede/dedup against
	Similarity float64 `json:"similarity"`
	Reason     string  `json:"reason"`
}

// Resolver is an optional pluggable strategy (e.g. an LLM-backed judge) that the
// Engine can defer to in the ambiguous band. Implementations must be safe for
// concurrent use.
type Resolver interface {
	Resolve(newContent string, candidate Fact) (Action, string, error)
}

// Engine applies a token-overlap heuristic, optionally deferring borderline
// cases to a Resolver.
//
// Intuition: facts about the *same thing* share most of their words and differ
// only in the changed value ("works at Google" → "works at OpenAI") — high but
// imperfect overlap. Facts about *different things* share few words. So:
//
//	overlap >= DupThreshold        → duplicate
//	ConflictThreshold..DupThreshold → conflict (supersede the closest)
//	< ConflictThreshold            → add
type Engine struct {
	DupThreshold      float64  // default 0.85
	ConflictThreshold float64  // default 0.45
	Resolver          Resolver // optional; consulted in the conflict band
}

// NewEngine returns an Engine with sensible defaults and no Resolver.
func NewEngine() *Engine {
	return &Engine{DupThreshold: 0.85, ConflictThreshold: 0.45}
}

// Resolve compares newContent against candidate facts (expected to be the active
// facts in the same category) and returns the recommended disposition.
func (e *Engine) Resolve(newContent string, candidates []Fact) Decision {
	newTokens := tokenize(newContent)
	if len(newTokens) == 0 || len(candidates) == 0 {
		return Decision{Action: ActionAdd, Reason: "no comparable existing facts"}
	}

	// Find the most similar existing fact.
	best := -1
	bestSim := 0.0
	for i, c := range candidates {
		sim := jaccard(newTokens, tokenize(c.Content))
		if sim > bestSim {
			bestSim, best = sim, i
		}
	}
	if best < 0 {
		return Decision{Action: ActionAdd, Reason: "no token overlap with existing facts"}
	}
	target := candidates[best]

	switch {
	case bestSim >= e.DupThreshold:
		return Decision{Action: ActionDuplicate, TargetID: target.ID, Similarity: bestSim,
			Reason: "near-identical to an existing fact"}

	case bestSim >= e.ConflictThreshold:
		// Borderline: an optional Resolver gets the final say.
		if e.Resolver != nil {
			if act, reason, err := e.Resolver.Resolve(newContent, target); err == nil {
				return Decision{Action: act, TargetID: target.ID, Similarity: bestSim, Reason: reason}
			}
			// Resolver failed — fall through to the heuristic (fail toward update,
			// which preserves history rather than accumulating contradictions).
		}
		return Decision{Action: ActionUpdate, TargetID: target.ID, Similarity: bestSim,
			Reason: "high overlap with differing detail — likely supersedes the prior fact"}

	default:
		return Decision{Action: ActionAdd, Similarity: bestSim, Reason: "low overlap — new information"}
	}
}

// ── token similarity ─────────────────────────────────────────────────────────

// stopwords are dropped so the overlap reflects content words, not grammar.
var stopwords = map[string]struct{}{
	"a": {}, "an": {}, "the": {}, "is": {}, "are": {}, "was": {}, "were": {},
	"i": {}, "you": {}, "he": {}, "she": {}, "it": {}, "we": {}, "they": {},
	"to": {}, "of": {}, "in": {}, "on": {}, "at": {}, "for": {}, "and": {},
	"my": {}, "your": {}, "their": {}, "as": {}, "with": {}, "that": {}, "this": {},
}

func tokenize(s string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, f := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		if len(f) < 2 {
			continue
		}
		if _, stop := stopwords[f]; stop {
			continue
		}
		out[f] = struct{}{}
	}
	return out
}

func jaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for t := range a {
		if _, ok := b[t]; ok {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// SortedTokens is a small helper exposed for debugging/inspection.
func SortedTokens(s string) []string {
	set := tokenize(s)
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}
