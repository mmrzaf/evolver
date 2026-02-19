# evolver

evolver is a drop-in GitHub Action that makes a repository “self-evolve” on a schedule.

## Cron Reliability

For long-running cron usage, evolver now keeps runtime health state and emits explicit alerts:

- `.evolver/state.json`: latest run status, timestamps, failure/no-op streaks, totals.
- `.evolver/runs.log`: append-only per-run event log (`start`, `changed`, `noop`, `error`).
- `.evolver/run.lock`: overlap protection with stale-lock recovery.

Default reliability behavior:

- Active lock older than `180` minutes is treated as stale and replaced.

Tune with environment variables:

- `EVOLVER_LOCK_STALE_MINUTES`
- `EVOLVER_STATE_FILE`
- `EVOLVER_RUN_LOG_FILE`
- `EVOLVER_LOCK_FILE`
