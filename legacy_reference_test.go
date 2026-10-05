package conflict

import (
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// legacyResolve is the decision logic as released in v0.1.0, kept as a reference
// so a test can prove the default configuration still behaves identically.
func legacyResolve(e *Engine, newContent string, candidates []Fact) Decision {
	newTokens := tokenize(newContent)
	if len(newTokens) == 0 || len(candidates) == 0 {
		return Decision{Action: ActionAdd, Reason: "no comparable existing facts"}
	}
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
		if e.Resolver != nil {
			if act, reason, err := e.Resolver.Resolve(newContent, target); err == nil {
				return Decision{Action: act, TargetID: target.ID, Similarity: bestSim, Reason: reason}
			}
		}
		return Decision{Action: ActionUpdate, TargetID: target.ID, Similarity: bestSim,
			Reason: "high overlap with differing detail — likely supersedes the prior fact"}
	default:
		return Decision{Action: ActionAdd, Similarity: bestSim, Reason: "low overlap — new information"}
	}
}

var referenceWords = strings.Fields("works lives uses prefers allergic manager editor project drink city Marisol Nadia Idris Ines Kenji Bellweather Okonkwo Ferreira Harbor Partners Cinder Labs Denver Boston Vim Emacs backend engineer user google openai shellfish coffee tea")

func randomSentence(r *rand.Rand) string {
	words := make([]string, 3+r.Intn(6))
	for i := range words {
		words[i] = referenceWords[r.Intn(len(referenceWords))]
	}
	return strings.Join(words, " ")
}

type randomResolver struct {
	act Action
	err bool
}

func (r randomResolver) Resolve(string, Fact) (Action, string, error) {
	if r.err {
		return ActionAdd, "", errors.New("down")
	}
	return r.act, "random", nil
}

func TestDefaultConfigurationMatchesTheReleasedAlgorithm(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	for trial := 0; trial < 20000; trial++ {
		candidates := make([]Fact, r.Intn(7))
		for i := range candidates {
			candidates[i] = Fact{ID: fmt.Sprintf("f%d", i), Content: randomSentence(r)}
		}
		e := NewEngine()
		e.ConflictThreshold = []float64{0.45, 0.2, 0.1}[r.Intn(3)]
		e.MaxCandidates = r.Intn(2)
		if r.Intn(3) > 0 {
			e.Resolver = randomResolver{act: Action(r.Intn(3)), err: r.Intn(4) == 0}
		}
		content := randomSentence(r)
		got, want := e.Resolve(content, candidates), legacyResolve(e, content, candidates)
		if got != want {
			t.Fatalf("default behavior changed for %q against %v\n got  %+v\n want %+v", content, candidates, got, want)
		}
	}
}
