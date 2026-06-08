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

### Known limitation
Very short antonym flips — "I love my job" → "I hate my job" — share only `job` after stopword removal (≈0.33), the *same* score as an additive change ("likes Python" → "likes Rust"). Lexically these are indistinguishable, so the bare heuristic conservatively returns `add` (never wrongly erases a fact). Resolving them correctly needs semantics — that's what the `Resolver` is for. Documented and tested rather than papered over by lowering the threshold (which would wrongly supersede additive facts).

## API

- `NewEngine() *Engine` — defaults: `DupThreshold 0.85`, `ConflictThreshold 0.45`
- `(*Engine).Resolve(newContent string, candidates []Fact) Decision`
- `Fact{ID, Content}` — adapt your own model into this
- `Decision{Action, TargetID, Similarity, Reason}`
- `Action`: `ActionAdd | ActionUpdate | ActionDuplicate`
- `Resolver` interface for optional semantic resolution

## License

MIT
