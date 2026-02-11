# Heisenberg

[![CI](https://github.com/kamilpajak/heisenberg/actions/workflows/ci.yml/badge.svg)](https://github.com/kamilpajak/heisenberg/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/kamilpajak/heisenberg?style=flat-square)](https://goreportcard.com/report/github.com/kamilpajak/heisenberg)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

AI-powered test failure analysis for GitHub repositories.

Heisenberg fetches Playwright test artifacts from GitHub Actions and uses Google Gemini to diagnose root causes of test failures.

## Features

- **Agentic analysis** - Iterative tool-calling loop (typically 4-7 rounds, max 20) gathers context automatically
- **Full CI access** - Fetches job logs, workflow files, test source code, and Playwright artifacts
- **Playwright support** - HTML reports (rendered via headless browser), blob reports, trace files
- **Root cause diagnosis** - AI-powered analysis with confidence scoring
- **Local dashboard** - Web UI for interactive analysis (`heisenberg serve`)
- **Progressive disclosure** - Suggests additional data sources when confidence is low

## Installation

### Homebrew (macOS/Linux)

```bash
brew install kamilpajak/tap/heisenberg
```

### Using Go

```bash
go install github.com/kamilpajak/heisenberg@latest
```

### From Source

```bash
git clone https://github.com/kamilpajak/heisenberg.git
cd heisenberg
go build -o heisenberg .
```

## Quick Start

1. Set up required environment variables:

```bash
export GITHUB_TOKEN=ghp_...      # GitHub token for artifact access
export GOOGLE_API_KEY=...        # Google AI API key for Gemini
```

2. Analyze a repository:

```bash
heisenberg owner/repo
```

## Usage

```bash
# Analyze the latest failed workflow run
heisenberg owner/repo

# Analyze with verbose output (shows LLM tool calls)
heisenberg owner/repo --verbose

# Analyze a specific workflow run
heisenberg owner/repo --run-id 12345678

# Output result as JSON
heisenberg owner/repo --json

# Start the local web dashboard
heisenberg serve

# Start dashboard on custom port
heisenberg serve --port 3000
```

### Example Output

```
  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Confidence: 85% ████████████████████░░░░ (low sensitivity)

The test failure in `auth.setup.ts` is caused by a timeout during
authentication setup. The login endpoint is returning HTTP 503,
indicating the backend service is unavailable during the test run.
```

## GitHub Action

Analyze test failures automatically when your CI fails:

```yaml
# .github/workflows/heisenberg.yml
name: Analyze Failures

on:
  workflow_run:
    workflows: ["CI"]
    types: [completed]

jobs:
  analyze:
    if: ${{ github.event.workflow_run.conclusion == 'failure' }}
    runs-on: ubuntu-latest
    steps:
      - uses: kamilpajak/heisenberg@v1
        with:
          google-api-key: ${{ secrets.GOOGLE_API_KEY }}
```

### Inputs

| Input | Required | Default | Description |
|-------|----------|---------|-------------|
| `google-api-key` | Yes | - | Google AI API key for Gemini |
| `repository` | No | Current repo | Repository to analyze (owner/repo) |
| `run-id` | No | Latest failed | Specific workflow run ID |

### Outputs

| Output | Description |
|--------|-------------|
| `diagnosis` | Analysis text |
| `confidence` | Confidence score (0-100) |
| `category` | Result category (diagnosis, no_failures, not_supported) |

The action writes a summary to the Job Summary page with the diagnosis and confidence score.

## Configuration

| Variable | Required | Description |
|----------|----------|-------------|
| `GITHUB_TOKEN` | Yes | GitHub personal access token with `repo` scope |
| `GOOGLE_API_KEY` | Yes | Google AI API key for Gemini access |

### macOS Keychain (Recommended)

Store tokens securely in Keychain and load with [direnv](https://direnv.net/):

```bash
# .envrc
export GITHUB_TOKEN=$(security find-generic-password -s "github-token" -w)
export GOOGLE_API_KEY=$(security find-generic-password -s "gemini-api-key" -w)
```

## How It Works

```
heisenberg owner/repo
       │
       ▼
┌─────────────────────────────┐
│  Fetch workflow run from    │
│  GitHub Actions             │
└─────────────┬───────────────┘
              │
              ▼
┌─────────────────────────────┐
│  Gemini analysis loop       │◄──────────┐
│  (typically 4-7 iterations) │           │
└─────────────┬───────────────┘           │
              │                           │
              ▼                           │
┌─────────────────────────────┐           │
│  Tool calls:                │           │
│  • get_job_logs             │           │
│  • get_artifact (HTML/blob) │           │
│  • get_test_traces          │   Need    │
│  • get_repo_file            │   more    │
│  • get_workflow_file        │  context  │
└─────────────┬───────────────┘           │
              │                           │
              ▼                           │
         ┌────────┐   No                  │
         │ Done?  │───────────────────────┘
         └────┬───┘
              │ Yes
              ▼
┌─────────────────────────────┐
│  Diagnosis with confidence  │
│  score (0-100%)             │
└─────────────────────────────┘
```

## Supported Test Frameworks

Currently optimized for:

- **Playwright** - HTML reports, blob reports, trace files

Other frameworks may work but are not fully supported yet.

## Privacy

Heisenberg sends test artifacts and job logs to Google's Gemini API for analysis. You provide your own API key. No data is stored or logged by this tool.

If your CI logs contain sensitive information, review Google's [Gemini API data usage policy](https://ai.google.dev/gemini-api/terms).

## Contributing

Contributions are welcome! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

This project uses a dual-license model:

| Component | License | Path |
|-----------|---------|------|
| CLI, analysis engine, integrations | [Apache 2.0](LICENSE) | `cmd/cli`, `pkg/*`, `internal/*` (most) |
| SaaS components (auth, billing, database) | [BSL 1.1](LICENSE_ENTERPRISE) | `internal/auth`, `internal/billing`, `internal/database`, `cmd/server` |

**Open Source (Apache 2.0):** You can freely use, modify, and distribute the CLI and analysis engine. This includes running `heisenberg` locally and in your CI pipelines.

**SaaS Components (BSL 1.1):** The authentication, billing, and multi-tenant database code cannot be used to operate a competing commercial service. After 4 years, these components convert to Apache 2.0.

For most users, only the Apache 2.0 license applies.
