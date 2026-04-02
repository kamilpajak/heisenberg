# Heisenberg

[![CI](https://github.com/kamilpajak/heisenberg/actions/workflows/ci.yml/badge.svg)](https://github.com/kamilpajak/heisenberg/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/kamilpajak/heisenberg?style=flat-square)](https://goreportcard.com/report/github.com/kamilpajak/heisenberg)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

AI-powered CI failure analysis for GitHub Actions, starting with Playwright and structured root-cause reports.

Bring your own Gemini key. Heisenberg analyzes workflow logs, artifacts, and traces to produce structured RCA reports — analysis data is not persisted by default.

## Why?

A failed CI run gives you 800 lines of logs and a red badge. Heisenberg reads those logs for you — plus artifacts, traces, and source code — and tells you **why** the test failed, **where** the bug is, and **how** to fix it. Locally, with your own API key, typically in under a minute.

## Example Output

Real output from analyzing a [Sage/carbon](https://github.com/Sage/carbon) CI failure:

```
  Analyzing run 23717508627 for Sage/carbon...
  Reading logs       1/30
  Reading source     2/30
  Finalizing         3/30
  ✓  Used 4/30 iterations

  Heisenberg — Sage/carbon #23717508627
  Branch: dependabot/npm_and_yarn/happy-dom-20.8.9   Event: pull_request
  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Confidence: 100% ████████████████████████

  INFRA in .github/workflows/pr.yml  [infrastructure]
  Bug location: .github/workflows/pr.yml

  Root Cause
  ────────────────────────────────────────
  The workflow attempts to trigger another workflow using a custom secret
  (`CHROMATIC_WORKFLOW_TOKEN`). Because this is a Dependabot PR, it does not
  have access to standard repository secrets. The missing secret results in
  an invalid or empty token being sent to the GitHub API, causing a 401 Bad
  Credentials error.

  Evidence
  ────────────────────────────────────────
  [Log]  The workflow is triggered by a Dependabot PR (branch:
         dependabot/npm_and_yarn/happy-dom-20.8.9).
  [Log]  The trigger-chromatic job fails with a 401 Bad credentials error
         when calling the GitHub API to dispatch the chromatic.yml workflow.
  [Code] The workflow file .github/workflows/pr.yml uses
         `secrets.CHROMATIC_WORKFLOW_TOKEN` for the GITHUB_TOKEN environment
         variable. Dependabot PRs do not have access to standard repository
         secrets unless explicitly added to Dependabot secrets.

  Fix
  ────────────────────────────────────────
  Add the `CHROMATIC_WORKFLOW_TOKEN` secret to the Dependabot secrets in the
  repository settings (Settings > Secrets and variables > Dependabot).
```

For large CI runs with many failures, Heisenberg pre-clusters them by error signature and analyzes each cluster separately:

```
  3 root causes found:

   1. [infra] TIMEOUT in auth.setup.ts:6 — Backend unavailable during test
   2. [test]  ASSERTION in checkout.spec.ts:45 — Stale CSS selector
   3. [production] ASSERTION in pricing.spec.ts:12 — Price calculation returns 0
```

## Quick Start

**Requirements:** a GitHub token and a Google AI API key ([get one here](https://aistudio.google.com/apikey)).

```bash
brew install kamilpajak/tap/heisenberg     # or: go install github.com/kamilpajak/heisenberg/cmd/cli@latest

export GITHUB_TOKEN=ghp_...
export GOOGLE_API_KEY=AIza...

heisenberg doctor                           # verify your setup
heisenberg analyze owner/repo               # analyze latest failed run
```

`heisenberg doctor` checks tokens, network connectivity, Playwright, and config:

```
  heisenberg doctor

  [OK] GITHUB_TOKEN is set (authenticated as @you)
  [OK] GOOGLE_API_KEY is set (validated)
  [OK] Network: api.github.com reachable
  [OK] Network: generativelanguage.googleapis.com reachable
  [OK] Playwright browser installed
  [INFO] Config file: not found (using defaults)
  [INFO] heisenberg v0.3.1

  5 passed
```

## Features

- **Detects root causes** from failed GitHub Actions runs — not just "test X failed" but *why* and *where*
- **Clusters related failures** automatically — 50 failing tests with the same cause become one diagnosis
- **Produces per-failure RCA** with evidence (traces, logs, screenshots) and actionable remediation
- **Supports Playwright traces**, HTML reports, and blob reports out of the box
- **Works locally and in CI** — run from your terminal or as a GitHub Action
- **Stable JSON output** — `--format json` with `schema_version` for pipelines and integrations

Under the hood: an agentic tool-calling loop (up to 30 iterations) lets Gemini decide what data it needs — job logs, artifacts, source files, trace recordings — before producing a structured diagnosis.

## How It Works

```
heisenberg analyze owner/repo
       │
       ▼
┌─────────────────────────────┐
│  Fetch workflow run from    │
│  GitHub Actions / Azure     │
└─────────────┬───────────────┘
              │
              ▼
┌─────────────────────────────┐
│  > 6 failed jobs?           │
│  Yes → pre-cluster by       │     ≤ 6 jobs
│        error signature      │──────────┐
└─────────────┬───────────────┘          │
              │ per cluster              │
              ▼                          ▼
┌─────────────────────────────┐ ┌───────────────────┐
│  Gemini analysis loop       │ │  Single Gemini    │
│  (per cluster, parallel)    │ │  analysis loop    │
└─────────────┬───────────────┘ └────────┬──────────┘
              │                          │
              ▼                          ▼
┌─────────────────────────────┐
│  Tool calls:                │
│  • list_jobs                │
│  • get_job_logs             │
│  • get_artifact (HTML/blob) │
│  • get_test_traces          │
│  • get_repo_file            │
│  • get_workflow_file        │
│  • get_pr_diff              │
└─────────────┬───────────────┘
              │
              ▼
┌─────────────────────────────┐
│  Structured RCA with        │
│  confidence, evidence,      │
│  and remediation            │
└─────────────────────────────┘
```

## Supported Frameworks

**Playwright** — full support: HTML reports, blob reports, trace files (browser actions, console errors, network requests).

**Other frameworks** — best-effort via generic log parsing. JUnit XML, pytest, and Go test output can be analyzed from job logs, but without deep artifact support. Framework-specific parsers are planned.

## Usage

```bash
# Analyze the latest failed workflow run
heisenberg analyze owner/repo

# Analyze a specific run by URL (paste from your browser)
heisenberg analyze -r https://github.com/org/repo/actions/runs/123456

# Analyze a specific run by ID
heisenberg analyze owner/repo --run-id 123456

# JSON output for CI pipelines
heisenberg analyze owner/repo -f json

# CI mode — read repo and run ID from GitHub Actions environment
heisenberg analyze --from-env

# Show detailed tool call info
heisenberg analyze owner/repo --verbose

# Override the Gemini model
heisenberg analyze owner/repo --model gemini-2.5-flash

# Azure DevOps — analyze by build URL
heisenberg analyze -r "https://dev.azure.com/org/project/_build/results?buildId=456"

# Azure DevOps — explicit org/project
heisenberg analyze --azure-org myorg --azure-project myproject --run-id 789

# Start the local web dashboard
heisenberg serve

# Check environment and configuration
heisenberg doctor
```

### JSON Output Schema

The `--format json` output includes a `schema_version` field for forward compatibility:

```json
{
  "schema_version": "1",
  "category": "diagnosis",
  "confidence": 85,
  "sensitivity": "low",
  "analyses": [
    {
      "title": "Timeout in beforeEach hook",
      "failure_type": "timeout",
      "location": { "file_path": "tests/inventory.spec.ts", "line_number": 12 },
      "bug_location": "test",
      "bug_location_confidence": "high",
      "root_cause": "CSS selector changed, loading indicator not found",
      "evidence": [{ "type": "trace", "content": "waitForSelector timed out" }],
      "remediation": "Update selector to match new data attribute",
      "fix_confidence": "high"
    }
  ],
  "run_id": 21575780517,
  "branch": "main"
}
```

Errors in JSON mode also return structured JSON (`{"schema_version": "1", "error": "...", "exit_code": 3}`), so CI pipelines always get parseable output.

## Privacy & Data Flow

Heisenberg runs locally. It reads artifacts and logs from GitHub's API, then sends relevant content to Google's Gemini API for analysis. **No data is persisted by Heisenberg itself** — there is no telemetry, no phoning home, no server component in the open-source CLI.

The content sent to Gemini (job logs, test output, trace excerpts) is subject to Google's [Gemini API data usage policy](https://ai.google.dev/gemini-api/terms). If your CI logs contain secrets or sensitive data, review that policy before use.

## GitHub Action

Analyze test failures automatically when your CI fails:

```yaml
# .github/workflows/heisenberg.yml
name: Analyze Failures

on:
  workflow_run:
    workflows: ["CI"]
    types: [completed]

permissions:
  contents: read
  actions: read

jobs:
  analyze:
    if: ${{ github.event.workflow_run.conclusion == 'failure' }}
    runs-on: ubuntu-latest
    steps:
      - uses: kamilpajak/heisenberg@v0.3.1
        with:
          google-api-key: ${{ secrets.GOOGLE_API_KEY }}
          run-id: ${{ github.event.workflow_run.id }}
```

The action writes the diagnosis to the [Job Summary](https://docs.github.com/en/actions/using-workflows/workflow-commands-for-github-actions#adding-a-job-summary) and exposes outputs for downstream steps:

| Output | Description |
|--------|-------------|
| `diagnosis` | Analysis text |
| `confidence` | Confidence score (0-100) |
| `category` | `diagnosis`, `no_failures`, or `not_supported` |

### Inputs

| Input | Required | Default | Description |
|-------|----------|---------|-------------|
| `google-api-key` | Yes | - | Google AI API key for Gemini |
| `github-token` | No | `github.token` | GitHub token for API access (override for cross-repo analysis) |
| `repository` | No | Current repo | Repository to analyze (owner/repo) |
| `run-id` | No | Latest failed | Specific workflow run ID |

## Configuration

### Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `GITHUB_TOKEN` | Yes | GitHub personal access token with `repo` scope |
| `GOOGLE_API_KEY` | Yes | Google AI API key for Gemini |
| `HEISENBERG_MODEL` | No | Override the default Gemini model |

### Config File (optional)

Heisenberg reads defaults from a YAML config file if present:

- **Linux:** `~/.config/heisenberg/config.yaml`
- **macOS:** `~/Library/Application Support/heisenberg/config.yaml`

```yaml
model: gemini-2.5-pro
github_token: ghp_...
google_api_key: AIza...
```

Precedence: CLI flag > environment variable > config file > default.

### macOS Keychain

Store tokens securely in Keychain and load with [direnv](https://direnv.net/):

```bash
# .envrc
export GITHUB_TOKEN=$(security find-generic-password -s "github-token" -w)
export GOOGLE_API_KEY=$(security find-generic-password -s "gemini-api-key" -w)
```

## Open Source & Licensing

This project uses a dual-license model:

| Component | License | What's included |
|-----------|---------|-----------------|
| CLI, analysis engine | [Apache 2.0](LICENSE) | `cmd/cli/`, `pkg/`, `internal/` — everything you need to run `heisenberg` locally and in CI |
| SaaS components | [BSL 1.1](ee/LICENSE) | `ee/` — authentication, billing, database, API server, web dashboard |

**The Apache 2.0 CLI will remain fully usable forever.** SaaS features (multi-user collaboration, history, hosted automation) live in the `ee/` directory under BSL and cannot be used to operate a competing commercial service. After 4 years, SaaS components convert to Apache 2.0.

For most users, only the Apache 2.0 license applies.

## Contributing

Contributions are welcome! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.
