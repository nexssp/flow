# Named bindings in a decision router

This example is adapted from the candidate preflight and decision stages in the attached `dec_final.nflow` router. It shows the `as` syntax on successful action outputs without changing the request payload or turning aliases into a result map. It assumes the `ai.cost.estimate` and `ai.decide` capabilities are mounted.

## Candidate cost preflight

```nflow
@pipeline try_candidate
  noop as state
  -> ai.cost.estimate @{
    .current.attempt.attempt_id,
    .current.attempt.model,
    .current.attempt.rate_card_id,
    .current.attempt.expected_currency,
    .current.attempt.prompt_tokens,
    .current.attempt.cached_prompt_tokens,
    .current.attempt.completion_tokens,
    prompt: .current.attempt
  } as estimate
  -> match {
    !estimate.Known -> {
      ... state,
      status: status.cand_preflight_unknown,
      stop: {
        candidate_id: state.current.id,
        state.current.attempt.attempt_id,
        issue: estimate.Issue,
        provider_called: false,
        fallback_suppressed: true
      }
    },
    estimate.CostMicros > state.remaining_budget_micros -> {
      ... state,
      status: status.cand_budget_rejected,
      stop: {
        candidate_id: state.current.id,
        state.current.attempt.attempt_id,
        estimated_cost_micros: estimate.CostMicros,
        remaining_budget_micros: state.remaining_budget_micros,
        provider_called: false,
        fallback_suppressed: true
      }
    },
    _ -> { ... state, estimate: estimate }
  }
@end
```

`noop`'s output still flows into `ai.cost.estimate`; `state` is only an additional name for the same output. The estimate action's output still flows into `match`, while `estimate.Known` and `estimate.CostMicros` read its bound value. The explicit final `estimate: estimate` is what returns the estimate to the caller; omitting it leaves the alias out of the result.

## Decision response

```nflow
ai.decide @{ ... .req.decision_payload } as decision
-> match {
  len(decision.Answers) == 0 -> { status: status.manual_review },
  _ -> { decision_choice: decision.Answers[0].Choice }
}
```

Here `decision.Answers[0].Choice` reads the immediate output of `ai.decide`. It does not change the action's normal `->` value flow, and it does not create a general `result` namespace. The runnable built-in-action sample is [`extensions/syntax/nflows/named_bindings.nflow`](../../extensions/syntax/nflows/named_bindings.nflow).
