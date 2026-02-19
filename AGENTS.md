# AGENTS.md

This file defines how coding agents should operate in this repository.

## Scope
- Applies to the entire repo unless a deeper `AGENTS.md` overrides it.

## Defaults
- Keep changes minimal and task-focused.
- Preserve existing code style and project conventions.
- Prefer small, reviewable patches over broad refactors.
- Optimize for long-term reliability, not just short-term success.

## Workflow
1. Inspect relevant files before editing.
2. Implement only what the task requires.
3. Run targeted checks/tests for touched areas when feasible.
4. Summarize what changed and any follow-up risks.

## Production Readiness
- Assume code will run in outside/real environments with variable network, load, and failures.
- Make critical operations idempotent so repeated runs do not break behavior.
- Add explicit timeouts, retries with backoff, and failure handling for external calls.
- Prefer safe recovery paths over crash-only behavior for expected transient faults.
- Preserve backward compatibility for persisted data, APIs, and config where possible.
- Add or update tests for repeat runs, restart behavior, and regression-prone paths.
- Include useful logs/metrics around failure points so production issues are diagnosable.
- Highlight single points of failure and note mitigation or follow-up work.

## Safety
- Do not delete or rewrite unrelated files.
- Do not revert user-authored changes unless explicitly asked.
- Call out assumptions when requirements are ambiguous.
