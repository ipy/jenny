## Non-negotiable rules

### Document-driven (mandatory order)

Every behavior change MUST follow this sequence — **never skip or reorder**:

1. **Documentation** — update or add spec under `docs/` (source of truth). read `docs/README.md` for format and guide
2. **Tests** — unit and integration tests (`*_test.go`) that encode acceptance criteria
3. **Code** — implementation that matches the spec and tests

If requirements are ambiguous, update the doc first; do not guess in code.

### Restriction on Documentation and Version Files

- **Do NOT create arbitrary documentation files** (CHANGELOG.md, etc.) other than specs under `docs/` unless explicitly requested by the user
- **Do NOT modify version numbers** (in code, config, or any file) unless explicitly requested by the user

### Guideline

The system is designed to be operated by AI agents. Clear file contracts, structured logs, deterministic state transitions.
Enforce minimal tech debt. Fewer, well-chosen dependencies. Delete code aggressively. Lowest abstract complexity.
