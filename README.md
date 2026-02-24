# evolver

A GitHub Action + Go CLI that proposes small, safe changes to your repo using Gemini, verifies them, and (when verification fails) performs **bounded repair attempts** before opening a PR or pushing directly.

evolver treats LLM output as **untrusted**:

* file edits are validated
* verification commands come from config/inputs (not the LLM)
* repair commands are **allowlisted project capabilities** selected by ID (not arbitrary shell)

## Quick start (PR mode)

```yaml
name: evolver

on:
  schedule:
    - cron: "0 9 * * 1" # Mondays 09:00 UTC
  workflow_dispatch:

permissions:
  contents: write
  pull-requests: write

jobs:
  evolve:
    runs-on: ubuntu-latest
    steps:
      - uses: mmrzaf/evolver@v1
        with:
          mode: pr
          gemini_api_key: ${{ secrets.GEMINI_API_KEY }}
          repo_goal: "Improve reliability and developer experience"
          commands: |
            go test ./...
            go vet ./...
```

## How it works

1. Gather repository context
2. Ask Gemini for a small change plan
3. Apply edits (with path/security validation)
4. Run verification commands
5. If verification fails:

   * classify failure
   * ask Gemini for a **repair plan**
   * optionally execute **project-allowed repair capabilities** (by ID)
   * re-run verification
   * stop after bounded attempts
6. Commit + push (or open PR)

## Verification vs Repair commands

evolver uses **two command layers**:

### Verification commands (always-run checks)

These are your normal checks and come from:

1. Action input `commands`
2. `.evolver/config.yml`
3. inferred defaults (fallback)

Examples:

* `go test ./...`
* `go vet ./...`
* `npm test`

### Repair capabilities (situational remediation)

These are **repo-defined allowlisted commands** the LLM may request **by capability ID** during repair mode.

Example:

* `go_mod_tidy` → `["go", "mod", "tidy"]`

This keeps evolver flexible across languages while preventing arbitrary shell execution.

## Project config (`.evolver/config.yml`)

Example Go project config with repair capabilities:

```yaml
provider: gemini
mode: push
model: gemini-2.5-flash
repo_goal: |
  Build evolved-commit, a zero-configuration CLI that ensures every Git commit is review-ready by installing safe Git hooks and running fast, opinionated checks with exact fix guidance. Changes are pushed directly to the default branch. One atomic improvement per change. Tests, docs, and changelog are mandatory. Offline, deterministic, fast. Always explain why + exact fix steps.
workdir: .

budgets:
  max_files_changed: 20
  max_lines_changed: 3000
  max_new_files: 20

commands:
  - go test ./...
  - go vet ./...

repair:
  max_attempts: 2
  max_actions_per_attempt: 2
  capabilities:
    - id: go_mod_tidy
      description: Sync go.mod and go.sum after dependency or import changes
      argv: ["go", "mod", "tidy"]
      timeout_seconds: 120
      max_runs_per_attempt: 1
      allowed_failure_kinds:
        - dependency_manifest_missing
        - dependency_fetch

allow_paths:
  - .

deny_paths:
  - .git/
  - .github/workflows/
  - node_modules/

security:
  allow_workflow_edits: false
  secret_scan: true

reliability:
  state_file: .evolver/state.json
  run_log_file: .evolver/runs.log
  lock_file: .evolver/run.lock
  lock_stale_minutes: 180
```

## Inputs

* `mode`: `pr` or `push` (default: `pr`)
* `provider`: currently only `gemini` (default: `gemini`)
* `model`: Gemini model name (default: `gemini-2.5-flash-lite`)
* `workdir`: directory to run in (default: `.`)
* `repo_goal`: high-level goal for the agent
* `commands`: newline-separated verification commands (run after changes)
* `allow_workflow_edits`: `"true"` to allow `.github/workflows` edits (default: `"false"`)
* `log_level`: `debug|info|warn|error` (default: `info`)
* `log_format`: `text|json` (default: `text`)
* `log_file`: path for persistent logs (default: `.evolver/evolver.log`)
* `gemini_api_key`: **required**, pass from secrets

## Outputs

* `changed`: `"true"` if a commit was made
* `summary`: short summary of the change
* `pr_url`: PR link (when `mode=pr`)

## Safety model

* LLM **cannot** directly execute arbitrary shell commands
* Verification commands are configured by the user/project
* Repair commands must be defined in `.evolver/config.yml` under `repair.capabilities`
* LLM may only request repair capability **IDs** from the allowed list
* evolver executes capability `argv` directly (no shell), with timeout and bounded runs

## Failure kinds

Failure classification is used to decide whether repair is attempted and which repair capabilities are exposed.

Current/common kinds include:

### Terminal / no auto-repair

* `security_integrity`

  * Examples: checksum mismatch, module verification security errors
  * Behavior: stop repair loop and fail safely

### Environment / infrastructure (usually not code-fixable)

* `env_command_missing`

  * Tool not installed / not on PATH
* `env_missing_path`

  * Missing file/dir required by command
* `env_network`

  * DNS/TLS/timeout/connectivity failures

### Dependency / package metadata

* `dependency_manifest_missing`

  * Example (Go): missing `go.sum` entry / updates to module metadata needed
  * Often repairable with a project capability like `go_mod_tidy`
* `dependency_fetch`

  * Dependency retrieval/auth issues; may or may not be repairable depending on repo/network setup

### Code / correctness

* `compile_failure`

  * Syntax/type/signature/build errors
* `test_failure`

  * Test assertions/panics/failing tests
* `vet_failure` / lint-style failures

  * Static analysis issues (exact kind may vary by project/tooling)
* `unknown_failure`

  * Unclassified command failure

### Important notes

* Command execution is generic; classification is heuristic/pattern-based.
* Non-Go projects may fall back to `unknown_failure` more often until you tune repair capabilities and (if needed) extend classifier patterns.
* If classification is uncertain, evolver should behave conservatively.

## Troubleshooting

### Verification failed and no repair happened

Possible reasons:

* failure kind is terminal (for example `security_integrity`)
* repair attempts exhausted
* no matching repair capabilities were allowed for that failure kind
* Gemini returned an invalid repair plan
* repair action exceeded limits (`max_actions_per_attempt`, `max_runs_per_attempt`)

What to check:

* `.evolver/evolver.log`
* configured `repair.capabilities`
* `allowed_failure_kinds` for the capability you expected to run

### Go error: missing `go.sum` entry / updates to `go.mod` or `go.sum` needed

This is the ideal case for a repair capability like:

* `go_mod_tidy` → `["go", "mod", "tidy"]`

Make sure it is present in `.evolver/config.yml` and allowed for:

* `dependency_manifest_missing`
* optionally `dependency_fetch`

### Go checksum mismatch / SECURITY ERROR

This should typically classify as `security_integrity` and stop auto-repair.

Why:

* the issue may indicate tampering, proxy inconsistency, or invalid checksums
* LLMs should not “invent” or patch checksums manually

What to do:

* inspect module source/version integrity
* verify proxy / checksum DB access
* re-run in a trusted environment
* confirm `go.sum` is not being hand-edited

### Tests fail repeatedly after repairs

Likely causes:

* insufficient repo context in repair prompt
* broad/incorrect LLM edits
* flaky tests
* hidden generated files / codegen requirements
* repair capabilities too weak (or too broad)

What helps:

* add targeted repair capabilities (carefully)
* improve repo context gathering / error-file refresh
* keep verify commands deterministic and fast

### Command works locally but fails in GitHub Actions

Check:

* missing tools in runner image
* PATH differences
* network access restrictions
* private dependency credentials
* working directory mismatch (`workdir` / capability `cwd`)

### Quoted verify commands behave oddly

If verify commands are defined as shell-like strings, quoting can be brittle.

Example problematic patterns:

* `pytest -k "my test"`
* commands relying on shell operators (`&&`, `|`, `;`)

Recommendation:

* keep verification commands simple and direct
* prefer project-defined repair capabilities using `argv` arrays for anything nontrivial

## Authoring repair capabilities

Repair capabilities are the core of safe agentic behavior. Treat them like a small, versioned API for the LLM.

### Design rules

* **Use stable IDs** (`go_mod_tidy`, `cargo_generate_lockfile`, `pnpm_install_lockfile`)
* **Use argv arrays**, not shell strings
* **Keep commands deterministic**
* **Set timeouts**
* **Scope by failure kind** with `allowed_failure_kinds`
* **Limit repeats** with `max_runs_per_attempt`

### Good capability examples

* Go:

  * `["go", "mod", "tidy"]`
  * `["go", "generate", "./..."]` (only if repo relies on it)
* Rust:

  * `["cargo", "generate-lockfile"]`
* Node (repo-specific; choose carefully):

  * lockfile sync commands if your workflow supports them

### Avoid these

* shell wrappers (`bash -lc`, `sh -c`)
* chained commands (`&&`, `;`, `|`)
* broad environment mutations
* tool installation commands (`go install`, package manager global installs)
* destructive cleanup commands
* commands that require secrets unless your CI environment is explicitly prepared

### Keep the list small

Start with **1–2 capabilities per repo**. Add more only after seeing repeated, legitimate failure patterns.

A small, reliable capability set beats a large, noisy one.

## Notes

* Keep repair capabilities small and deterministic
* Start with 1–2 capabilities per repo (example: `go_mod_tidy`)
* Do not add broad or shell-wrapped commands (`bash -lc`, `sh -c`, `go get ...`, etc.)
* Failure classification is generic execution + language-specific pattern matching; tune capabilities per project
* Repair attempts are bounded to avoid infinite loops

## Roadmap ideas

* Verify commands as `argv` arrays (to avoid quoting issues)
* Config-driven failure signature mappings per project/language
* Targeted repo-context refresh using files implicated by errors
* Richer repair action types beyond command capabilities (still allowlisted)
* Stronger tests for repair policy enforcement and capability execution limits

