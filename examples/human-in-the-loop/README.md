# Example — Human-in-the-loop (MCP elicitation)

A publish-approval workflow that **pauses mid-DAG to ask a real person** three
questions, then feeds their answers into the rest of the graph:

```
draft ──► confirm      (HumanBoolInputOp   → approved:bool)  ─┐
      ──► action_choice (HumanChoiceInputOp → action_idx:int) ─┼──► decide (DecisionOp → summary)
      ──► note          (HumanInputOp       → note:string)    ─┘
```

It exercises all three human-in-the-loop ops:

- **`HumanBoolInputOp`** — a yes/no question yielding a `bool`.
- **`HumanChoiceInputOp`** — pick one of a fixed list, yielding the **zero-based
  index** of the chosen option (here into `deliveryOptions`).
- **`HumanInputOp`** — free-form text.

No API key is required — every op is deterministic apart from the human.

## How the human is reached

The ops don't know *how* to ask a person; they pull a `library.HumanPrompter`
off the run context. The entrypoint installs the right one per mode:

| Mode | Prompter | The human answers… |
|------|----------|--------------------|
| `--mcp` (MCP server) | `library.ElicitPrompter(req.Session)` | in their MCP client's UI, via `elicitation/create` requests |
| CLI (default)        | `library.StdinPrompter()`             | on the terminal |

`WithHumanPrompter` wraps whichever prompter you install in a **per-run
serializer**, so the three ops — which dagor runs in parallel — never prompt the
human at the same time. That's why they don't need to be chained in the DAG.
The serializer guarantees *one question at a time*, not a fixed *order*: which
of the three appears first is up to the scheduler. If a specific order matters,
add an ordering edge, e.g. `.Condition("always").ConditionInput("approved")` on
the later op.

## Run it

### CLI (interactive)

```bash
go run . --draft "Ship v2 to all users"
```

Answer each prompt as it appears (they may appear in any order). Example
session:

```
Approve this draft for delivery?

Ship v2 to all users [y/n]
> y
How should it be delivered?

Ship v2 to all users
  1) send now
  2) schedule for later
  3) save as draft
> 2
Add a short note to accompany it (leave blank for none):
> double-check the changelog first
```

```json
{
  "draft": "Ship v2 to all users",
  "summary": "Approved — \"Ship v2 to all users\" will schedule for later. Note: double-check the changelog first"
}
```

> Piping positional answers (`printf 'y\n2\n…' | …`) is unreliable here: the
> prompts are independent and may run in any order, so a positional script can
> pair answers with the wrong question. Answer interactively instead.

### MCP server

```bash
go run . --mcp
```

Speaks newline-delimited JSON-RPC over stdin/stdout and exposes one tool,
`review_and_act`. When the workflow hits a HITL op it sends the client an
`elicitation/create` request; the client must support the **elicitation**
capability (otherwise the op fails with "client does not support
elicitation").

## Test

```bash
go test ./examples/human-in-the-loop/
```

The tests drive `runWorkflow` with a content-aware fake prompter, so they're
deterministic regardless of the order the parallel HITL ops run in.
