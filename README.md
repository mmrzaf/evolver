# evolver

A GitHub Action + Go CLI that proposes small, safe changes to your repo using Gemini, then either opens a PR or pushes directly.

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

## Inputs

- `mode`: `pr` or `push` (default: `pr`)
- `provider`: currently only `gemini` (default: `gemini`)
- `model`: Gemini model name (default: `gemini-2.5-flash-lite`)
- `workdir`: directory to run in (default: `.`)
- `repo_goal`: high-level goal for the agent
- `commands`: newline-separated verify commands (run after changes)
- `allow_workflow_edits`: `"true"` to allow `.github/workflows` edits (default: `"false"`)
- `gemini_api_key`: **required**, pass from secrets

## Outputs

- `changed`: `"true"` if a commit was made
- `summary`: short summary of the change
- `pr_url`: PR link (when `mode=pr`)
