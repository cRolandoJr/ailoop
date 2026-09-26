# Golden cases

Each `.json` file is a case: a task whose answer is known in advance, and the checks the
circuit's output has to satisfy.

They live here and not under `.ailoop/` because `.ailoop/` is runtime state and is
gitignored. A case decides a verdict, so it has to be reviewable in a diff. Runs *are*
state, and they go to `.ailoop/golden-runs.jsonl`.

```json
{
  "slug": "unique-identifier",
  "title": "what this case protects",
  "phase": "DISCOVERY | DESIGN | PLAN | IMPLEMENTATION | VERIFICATION",
  "task": "what the agent is asked to do",
  "checks": [ { "contains": "..." } ],
  "disabled": false
}
```

A check asserts **exactly one** of `contains`, `not_contains` or `regex`, and the code
interprets it — never a model. An LLM judge puts inside the instrument the very variance
the instrument exists to detect.

These fail at LOAD time, not halfway through a run: a case with no checks, a check that
asserts nothing, a check with two assertions, a regex that does not compile, and two cases
sharing a slug. An empty suite **is not green**.

A case the circuit stops catching is a finding about the circuit, in the same way a
surviving mutant invalidates the test. You adjust the prompt or the phase, not the case.

Run with `ailoop golden "optional note"`. It spends real model calls.
