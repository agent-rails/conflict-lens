# conflict-lens

A tiny, dependency-free Go library that decides what a **new fact** means relative to what's already known: `add`, `update` (supersede), or `duplicate`.

```go
import conflict "github.com/voltagebots/conflict-lens"

e := conflict.NewEngine()
d := e.Resolve("User works at OpenAI", []conflict.Fact{
    {ID: "1", Content: "User works at Google"},
})
// d.Action == ActionUpdate, d.TargetID == "1"  → supersede the old fact
```

## Why

Retrieval (vector or keyword) measures **similarity, not truth**. "I love my job" and "I quit" both mention the job and come back together — a naive agent invents a synthesis. conflict-lens classifies the *relationship* so your store can supersede the stale fact (keeping it as history) instead of accumulating contradictions.

It is the reusable core extracted from [memkit](https://github.com/voltagebots/memkit). Zero dependencies — drop it into any memory system.

## How it works

Token-overlap (Jaccard) between the new fact and each existing candidate, with stopwords removed:

```
overlap ≥ 0.85            → duplicate (skip / refresh)
0.45 ≤ overlap < 0.85     → conflict  (supersede the closest) ← Resolver consulted here
overlap < 0.45            → add       (new information)
```

Intuition: facts about the *same thing* share most words and differ in the changed value ("works at **Google**" → "works at **OpenAI**", ≈0.67). Different facts share few words.

Thresholds are fields on `Engine`, so you can tune them.

## Optional LLM Resolver

The heuristic is cheap and handles the common, clear cases at zero marginal cost. For the ambiguous middle band, plug in a semantic judge:

```go
type Resolver interface {
    Resolve(newContent string, candidate Fact) (Action, string, error)
}

e := conflict.NewEngine()
e.Resolver = myClaudeResolver // consulted only in the conflict band
```

### Examining several candidates

By default the Resolver sees only the single most similar candidate. When that candidate may be about a different subject than the new fact ("Nadia works at Acme" against "Marisol works at Acme"), a perfect judge can only answer "add" and the fact that is really superseded is never examined. Set `MaxCandidates` above one to let the Resolver examine the top candidates in order, most similar first, at or above `ConflictThreshold`:

```go
e := conflict.NewEngine()
e.Resolver = myResolver
e.ConflictThreshold = 0.1 // let weaker overlaps qualify
e.MaxCandidates = 10      // at most ten judge calls per fact
```

The first candidate the Resolver does not call `add` decides the outcome. With `MaxCandidates` above one, any Resolver error or undefined action adds the fact and supersedes nothing, so a failing Resolver can never erase a stored fact. With the default (zero or one) behavior is exactly as before, including the heuristic fallback when the Resolver errors; a randomized differential test against the previous release checks this.

A Resolver may also implement `MultiResolver` (`ResolveAmong`) to judge all candidates in one call. An invalid answer, or an error, adds the fact. In one evaluation with an 8B local model, a single batched call over ten candidates proposed a replacement for unrelated facts far too often; pairing it with a per-candidate check removed that damage but left more facts stale than judging candidates one at a time. Measure it with your own model before relying on it.

### Known limitation
Very short antonym flips — "I love my job" → "I hate my job" — share only `job` after stopword removal (≈0.33), the *same* score as an additive change ("likes Python" → "likes Rust"). Lexically these are indistinguishable, so the bare heuristic conservatively returns `add` (never wrongly erases a fact). Resolving them correctly needs semantics — that's what the `Resolver` is for. Documented and tested rather than papered over by lowering the threshold (which would wrongly supersede additive facts).

### Subject confusion

The overlap score counts every shared word, including a person's name. Two different people who share a surname and a value ("Marisol Bellweather works at Harbor Partners", "Nadia Bellweather works at Harbor Partners") can score above the conflict threshold, so the bare heuristic may supersede the wrong person's fact. On a held-out synthetic evaluation with an Ollama judge (n=102 per category, one run, one 8B model), another person's fact was wrongly missing from the top five in 72% of cases with the heuristic and 8% with the judge, but stale facts were not reduced in general because the right fact was often outside the top candidates. Data, protocol and limits: [agent-rails/memkit pull request 2](https://github.com/agent-rails/memkit/pull/2), file `eval/EVAL_V2.md`.

## API

- `NewEngine() *Engine` — defaults: `DupThreshold 0.85`, `ConflictThreshold 0.45`, `MaxCandidates 0` (best candidate only)
- `(*Engine).Resolve(newContent string, candidates []Fact) Decision`
- `Fact{ID, Content}` — adapt your own model into this
- `Decision{Action, TargetID, Similarity, Reason}`
- `Action`: `ActionAdd | ActionUpdate | ActionDuplicate`
- `Resolver` interface for optional semantic resolution; `MultiResolver` for judging several candidates in one call

## License

MIT
