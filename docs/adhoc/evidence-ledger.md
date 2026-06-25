---
title: Evidence Ledger + complete_step Verification
slug: evidence-ledger
priority: P3
status: not_started
spec: complete
code: not_started
package: internal/agent, internal/tool
depends_on:
  - agent-loop
  - tool-registry
  - progress-monitor
---
# Evidence Ledger + complete_step Verification

## Problem

Jenny's QA Gate verifies deliverables at `end_turn` using a full verification sub-agent. This is effective but heavy — it only fires for sessions ≥20 turns and costs a full API call. For shorter sessions, or for incremental verification during a long session, there is no mechanism to verify that the agent actually ran the validation commands it claims to have run.

Common failure mode: the agent writes code, declares "I've verified it works" in its text output, but never actually ran tests or build commands. The QA Gate catches this at the end, but by then the agent may have moved on to other changes built on unverified assumptions.

## Design: Evidence Ledger

### Concept

An **evidence ledger** records receipts of tool executions during the session. When the agent attempts to mark a step as complete (via a `complete_step` tool or end_turn), the ledger can verify that claimed verification actions were actually performed.

### Ledger Structure

The ledger is an append-only log of tool execution receipts:

```
Receipt {
    TurnNumber   int       // Which API turn this occurred in
    ToolName     string    // e.g., "Bash"
    CommandHash  string    // For Bash: hash of the command string
    ExitCode     int       // For Bash: exit code (0 = success)
    IsError      bool      // Tool-level error flag
    FilePath     string    // For Read/Write/Edit: the file path
    Timestamp    time.Time
}
```

The ledger is maintained in-memory during the session (not persisted to transcript, to avoid bloat).

### complete_step Tool

A new tool that the model can call to signal completion of a logical step:

```json
{
    "name": "complete_step",
    "description": "Signal completion of a logical step. Requires specifying the verification command that validates this step's work. The command must have been executed successfully in the current session.",
    "input_schema": {
        "type": "object",
        "properties": {
            "step": {
                "type": "string",
                "description": "Description of the completed step"
            },
            "verification": {
                "type": "string",
                "description": "The exact bash command that verifies this step (must have been run with exit code 0)"
            }
        },
        "required": ["step", "verification"]
    }
}
```

When called:
1. Look up the `verification` command in the ledger (by command hash or substring match)
2. If found with exit code 0 → accept, return success
3. If found with non-zero exit code → reject with "verification command failed"
4. If not found → reject with "verification command not found in session history; run it first"

### Integration with Progress Monitor

Successful `complete_step` calls contribute a strong positive signal (+3) to the Progress Monitor, similar to a test pass. This allows the agent to signal progress on tasks that don't match the heuristic scoring prefixes (e.g., SQL tuning, infrastructure work).

### Integration with QA Gate

The QA Gate can check the evidence ledger as part of its verification:
- If the ledger shows the agent called `complete_step` with passing verification for all major steps → lightweight QA (or skip)
- If the ledger shows unverified steps → full QA verification

This allows the QA Gate to be proportional to the evidence already collected.

### System Prompt Integration

When `complete_step` is registered as a tool, the system prompt should include guidance:

```
After completing a significant step (implementing a feature, fixing a bug, refactoring), 
use complete_step to record your progress. Specify the bash command you used to verify 
the work (e.g., "go test ./...", "npm test", "cargo build"). The verification command 
must have been executed successfully in this session.
```

### Scope and Limitations

- The ledger is **session-scoped** (cleared on new session, not persisted to transcript)
- Command matching uses **normalized comparison** (trim whitespace, collapse runs of spaces) rather than exact string equality
- The ledger caps at **500 receipts** (LRU eviction of oldest entries)
- `complete_step` is **optional** — the agent can still function without using it
- The tool should only be registered when the session has a non-trivial task (e.g., when complexity eval says `need_write: true`)

### Example Flow

```
Turn 3: Agent writes auth middleware → Edit tool → receipt logged
Turn 4: Agent runs `go test ./auth/...` → Bash exit 0 → receipt logged
Turn 5: Agent calls complete_step(step="add auth middleware", verification="go test ./auth/...")
         → Ledger finds the Bash receipt with exit 0 → ✓ accepted
Turn 8: Agent tries complete_step(step="add rate limiting", verification="go test ./ratelimit/...")
         → Ledger has no matching receipt → ✗ rejected, "run the verification first"
```

## Acceptance Criteria

- Evidence ledger records all Bash and file-operation receipts during a session
- `complete_step` validates the verification command against the ledger
- Missing or failed verification produces a clear, actionable error
- Successful `complete_step` contributes +3 to Progress Monitor scoring
- Ledger is bounded at 500 entries with LRU eviction
- No transcript persistence overhead (in-memory only)
- Tool registration is conditional (only for write-requiring tasks)

## Risks

| Risk | Mitigation |
|------|-----------|
| Model ignores complete_step | Tool is optional; QA Gate remains as the final safety net |
| Model fabricates verification commands | Ledger checks actual execution history, not model claims |
| Ledger size grows unbounded | 500-entry LRU cap |
| Command matching too strict | Normalized comparison + substring matching for flexibility |
| Overhead from ledger bookkeeping | In-memory append-only log; negligible performance impact |
