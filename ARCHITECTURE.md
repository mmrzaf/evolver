# ARCHITECTURE.md

## 1. Project summary

**evolver** is a drop-in GitHub Action that makes a repository “self-evolve” on a schedule. A target repo only needs a single workflow step:

```yaml
- uses: mmrzaf/evolver@v1
  with:
    mode: pr
    gemini_api_key: ${{ secrets.GEMINI_API_KEY }}
    repo_goal: "Build a small useful Go CLI for developers"
```

On each run, the Action:

1. **Bootstraps** missing control files (`POLICY.md`, `ROADMAP.md`, `.evolver/config.yml`, `CHANGELOG.md`)
2. Computes repo context
3. Asks Gemini for a **strict JSON plan** describing small changes
4. Enforces policy + budgets
5. Applies changes
6. Runs configured checks/tests
7. Creates a **PR** by default (or pushes if explicitly configured)

The system is designed to be **incremental, constrained, auditable, and safe-by-default**.

---

## 2. Goals and non-goals

### Goals

- **One-file installation** for target repos (only add a workflow).
- **PR-first** evolution with clear diffs and review.
- **Small, incremental changes** driven by a roadmap.
- Strong guardrails:
  - deny-paths (workflows, git internals)
  - secret/sensitive content prevention
  - change budgets (files/LOC)
  - command-based verification (tests/linters)

- Deterministic, reproducible execution in GitHub Actions.

### Non-goals

- Full autonomy without guardrails (no “random chaos”).
- Direct main-branch mutation by default.
- Editing GitHub workflows by default.
- Multi-provider LLM support in v1 (Gemini-only in v1).

---

## 3. User experience (target repo)

### 3.1 Minimal required setup

Target repo must have:

- A workflow file referencing the action `mmrzaf/evolver@v1`
- GitHub Actions secret `GEMINI_API_KEY`

Nothing else is required; the action creates missing control files.

### 3.2 Typical workflow file

`.github/workflows/evolver.yml`:

```yaml
name: evolver
on:
  schedule:
    - cron: "17 3 * * *"
  workflow_dispatch: {}

permissions:
  contents: write
  pull-requests: write

concurrency:
  group: evolve-${{ github.repository }}
  cancel-in-progress: false

jobs:
  evolve:
    runs-on: ubuntu-latest
    steps:
      - uses: mmrzaf/evolver@v1
        with:
          mode: pr
          model: gemini-2.5-flash-lite
          repo_goal: "Make a small developer tool that solves a real pain"
        env:
          GEMINI_API_KEY: ${{ secrets.GEMINI_API_KEY }}
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

---

## 4. Repository contract (files evolver manages)

All files below are **created if missing** during `bootstrap`.

### 4.1 `POLICY.md` (hard rules)

Purpose: non-negotiable constraints governing what changes are allowed.

**Default rules (baseline)**

- Must:
  - Make small incremental changes.
  - Keep the repo runnable.
  - Update `CHANGELOG.md` every run.
  - Keep `ROADMAP.md` aligned with the repo direction.
  - Add tests when adding behavior.

- Must not:
  - Add secrets/credentials/private keys.
  - Modify `.github/workflows/**` (unless explicitly allowed).
  - Delete large portions of the repo.
  - Add malware/exploits/illegal content.

- Limits (also enforced by config):
  - `<= 10 files changed`
  - `<= 500 lines changed`

**Enforcement**: hard-fail if violated.

### 4.2 `ROADMAP.md` (objective function)

Purpose: defines what “good” means and what the agent should do next.

**Default structure**

- **Current Objective** (single sentence)
- **Now / Next / Later** checklist
- **Definition of Done** for the current objective

### 4.3 `.evolver/config.yml` (execution + budgets)

Purpose: the machine-readable config controlling budgets, commands, allowed paths, and run mode.

**Schema**

```yaml
provider: gemini
model: gemini-2.5-flash-lite

mode: pr # pr|push
workdir: "." # optional

budgets:
  max_files_changed: 10
  max_lines_changed: 500
  max_new_files: 10

commands:
  - go test ./... # example; may be inferred if missing

allow_paths:
  - "."

deny_paths:
  - ".git/"
  - ".github/workflows/"
  - "node_modules/"

security:
  allow_workflow_edits: false
  secret_scan: true
```

### 4.4 `CHANGELOG.md` (audit trail)

Purpose: immutable run-by-run record of changes.

**Default format**

- Append one bullet per run:
  - `- YYYY-MM-DD: <summary> (mode: pr|push, commands: pass|fail)`

### 4.5 `.evolver/state.json` (internal state)

Purpose: keep short memory of prior runs to avoid repetition and to continue work coherently.

**Schema**

```json
{
  "last_run_utc": "2026-02-19T03:17:00Z",
  "last_summary": "Added CLI skeleton and selfcheck",
  "last_branch": "evolver/2026-02-19",
  "recent_objectives": [
    "Initialize Go CLI scaffold",
    "Add basic command structure"
  ],
  "cooldowns": {
    "topic:logging": "2026-03-05T00:00:00Z"
  }
}
```

State is updated only on successful plan application (and successful verification in PR/push mode).

---

## 5. Action interface

### 5.1 Inputs (Action `with:`)

Minimal, high-leverage inputs:

| Input                  | Type             |                 Default | Description                                                 |                                      |
| ---------------------- | ---------------- | ----------------------: | ----------------------------------------------------------- | ------------------------------------ |
| `mode`                 | `pr              |                   push` | `pr`                                                        | PR by default; push only if explicit |
| `provider`             | string           |                `gemini` | v1 supports `gemini` only                                   |                                      |
| `model`                | string           | `gemini-2.5-flash-lite` | Gemini model name                                           |                                      |
| `repo_goal`            | string           |                    `""` | Seed intent, especially for empty repos                     |                                      |
| `workdir`              | string           |                     `.` | Subdirectory to operate in                                  |                                      |
| `max_files_changed`    | int              |                    `10` | Budget override                                             |                                      |
| `max_lines_changed`    | int              |                   `500` | Budget override                                             |                                      |
| `commands`             | multiline string |                    `""` | Overrides config commands                                   |                                      |
| `allow_workflow_edits` | bool             |                 `false` | If true, lifts deny rule for workflows (still policy-gated) |                                      |

**Precedence**

1. Action inputs (explicit)
2. `.evolver/config.yml`
3. Defaults (fallback)

### 5.2 Environment variables

| Env              |       Required | Purpose                          |
| ---------------- | -------------: | -------------------------------- |
| `GEMINI_API_KEY` |            yes | LLM access                       |
| `GITHUB_TOKEN`   | yes (provided) | create branches/PRs/push commits |

### 5.3 Outputs

| Output    | Description                       |        |
| --------- | --------------------------------- | ------ |
| `changed` | `true                             | false` |
| `summary` | one-line run summary              |        |
| `pr_url`  | PR URL if created (empty if none) |        |

---

## 6. High-level lifecycle

### 6.1 `bootstrap` phase

1. Validate repository and permissions.
2. Create missing control files with sane defaults.
3. If repo is empty (no meaningful files):
   - Use `repo_goal` if present
   - Else infer a goal from repo name + a general “useful starter project” direction

4. Write/refresh `.evolver/config.yml` only if missing.
5. Never overwrite user-edited policy/roadmap unless explicitly asked by plan (and plan passes policy).

### 6.2 `run` phase

1. Snapshot current repo state (paths, sizes, hashes, key excerpts).
2. Create an LLM prompt with:
   - policy text
   - roadmap text
   - config budgets and deny paths
   - recent changelog tail
   - recent objectives from `.evolver/state.json`
   - a compact repository inventory

3. Request a **strict JSON plan**.
4. Validate plan:
   - JSON parse
   - path allow/deny enforcement
   - budget enforcement
   - secret scan on generated content
   - “no workflow edits” rule unless allowed

5. Apply plan changes (file writes).
6. Run commands (tests/linters).
7. If commands pass:
   - PR mode: branch, commit, push, open PR
   - Push mode: commit and push to default branch

8. Append changelog entry.
9. Update state.

If any step fails, **no commit** is created.

---

## 7. Plan format (LLM contract)

The model must return **only valid JSON**. Any extra text is a hard failure.

### 7.1 Plan schema

```json
{
  "summary": "one sentence describing the change",
  "files": [
    {
      "path": "relative/path",
      "mode": "write",
      "content": "full file content"
    }
  ],
  "changelog_entry": "- YYYY-MM-DD: ...",
  "roadmap_update": "optional full ROADMAP.md content"
}
```

### 7.2 Supported file operations

v1 supports only:

- `write` (replace or create)

No arbitrary patch format, no partial edits. This keeps behavior deterministic and limits complexity.

### 7.3 Invalid-plan handling

- Attempt **one** retry with a “JSON-only” correction prompt.
- If still invalid, fail the job with a clear error.

---

## 8. Policy and guardrails

### 8.1 Path controls

- Default deny:
  - `.git/**`
  - `.github/workflows/**`
  - `node_modules/**`

- Configurable via `.evolver/config.yml` and Action inputs.
- If `allow_workflow_edits=true`, workflow paths become allowed **only if**:
  - POLICY explicitly permits it, or
  - the plan contains an explicit justification and policy validator accepts it (implementation choice: v1 keeps it simple—require policy opt-in).

### 8.2 Budget enforcement

Enforced after applying changes but before committing:

- `max_files_changed`
- `max_lines_changed`
- `max_new_files`

**Line counting method**

- Use `git diff --numstat` to compute insertions/deletions (preferred).
- Fall back to approximate line counts only if git diff is unavailable (rare in Actions).

If budgets exceed: hard-fail and do not commit.

### 8.3 Sensitive data prevention

Enable when `security.secret_scan=true`.

Blocks:

- Private key blocks (PEM headers)
- Known credential patterns (AWS keys, GitHub tokens, etc.)
- High-entropy token-like strings (heuristic threshold)

Additionally:

- Never include environment variables in prompt.
- Redact secrets in logs (do not print env, do not dump request headers).

### 8.4 Content safety constraints

The tool rejects plans that:

- introduce malware/exploit code
- attempt credential exfiltration
- modify workflows to run unauthorized steps (especially if workflow edits are enabled)

---

## 9. Verification strategy (commands)

### 9.1 Command selection

Priority:

1. Action input `commands`
2. Config `.evolver/config.yml` `commands`
3. Inference fallback:
   - Go: `go test ./...`
   - Node: `npm test` if package.json exists
   - Python: `python -m pytest` if pytest config exists
   - If none possible: generate initial scaffold including a runnable check (e.g., a minimal test script)

### 9.2 Failure behavior

If any command fails:

- Do not commit
- Fail the job (so the user notices)
- Store logs in Action output and step logs

---

## 10. Git and PR behavior

### 10.1 Branch naming

`evolver/YYYY-MM-DD`
If branch exists, append a short suffix (e.g., `evolver/2026-02-19-2`).

### 10.2 Commit identity

Use a stable bot identity:

- name: `repo-evolver`
- email: `repo-evolver@users.noreply.github.com`

### 10.3 PR template

PR body includes:

- Summary
- Files changed count and line stats
- Commands executed + results
- Roadmap alignment: “Which roadmap item did this move forward?”
- Next recommended step

### 10.4 No-op behavior

If no changes:

- Exit success
- Output `changed=false` and a short summary

---

## 11. Logging and observability

### 11.1 Logs

- Always log:
  - chosen mode
  - budgets
  - changed file list (paths only)
  - command results (pass/fail)

- Never log:
  - raw secrets
  - full LLM request/response that might include sensitive repo content (default: store only a compact trace)

### 11.2 Artifacts (optional future)

v1 can skip artifacts. If enabled later:

- upload sanitized plan JSON
- upload command logs

---

## 12. Packaging and release

### 12.1 Action type

**Composite action** that downloads and runs a pinned release binary.

Why:

- No Go toolchain required in target repos
- Faster execution
- Stable behavior pinned to tag (`@v1`)

### 12.2 Binary distribution

- GitHub Releases assets for:
  - `linux/amd64` (required for Actions)
  - optional: `linux/arm64`, `darwin/*`, `windows/*` for local runs

### 12.3 Integrity

- Publish `SHA256SUMS` per release
- Installer verifies checksum before executing

### 12.4 Versioning

- SemVer releases: `v1.0.0`, `v1.0.1`, ...
- Floating major tag: `v1` updated to the latest `v1.x.y`

---

## 13. Internal implementation (Go CLI)

### 13.1 Repo structure (mmrzaf/evolve)

```
/
  action.yml
  dist/
    install.sh
  cmd/
    evolver/
      main.go
  internal/
    config/
    policy/
    repoctx/
    llm/
      gemini/
    plan/
    apply/
    verify/
    gitops/
    ghapi/
    security/
```

### 13.2 Key modules

- `config`: load/merge config and action inputs
- `policy`: parse and enforce policy rules
- `repoctx`: build compact repo inventory for prompts
- `llm/gemini`: Gemini client, JSON-only prompting, retry logic
- `plan`: validate plan schema, budgets, paths
- `apply`: write files safely, ensure directories exist
- `verify`: run commands, capture outputs
- `gitops`: commit, branch, diff stats
- `ghapi`: create PR using GitHub REST API
- `security`: secret scanning + redaction

---

## 14. Prompting strategy

### 14.1 Prompt contents (minimum)

- POLICY.md (full)
- ROADMAP.md (full)
- CHANGELOG tail (last ~2k chars)
- Config budgets + deny paths
- Repo inventory:
  - file list (capped)
  - key file excerpts (capped)
  - language signals (go.mod, package.json, etc.)

- State:
  - recent objectives
  - avoid repetition hints

### 14.2 Prompt constraints

- Explicitly instruct:
  - “Return only JSON”
  - “Small change”
  - “No workflows edits”
  - “Add tests when adding behavior”
  - “Stay under budgets”

---

## 15. Security model

### 15.1 Principle of least privilege

Workflow permissions:

- `contents: write` (needed for pushing branches)
- `pull-requests: write` (needed for PRs)

No other permissions required by default.

### 15.2 Secret handling

- GEMINI_API_KEY lives only as GitHub secret.
- Never written to files.
- Never included in prompts.
- Never printed.

### 15.3 Trust boundaries

- The LLM is untrusted input.
- Every LLM output is validated and gated by policy + budgets + scans.

---

## 16. Extensibility roadmap (post-v1)

- Provider interface to support other LLMs
- Patch-based edits (carefully) for large repos
- Better test inference and language toolchains
  … [TRUNCATED: original_lines=633 kept_lines=600]
