# Heisenberg

[![CI](https://github.com/kamilpajak/heisenberg/actions/workflows/ci.yml/badge.svg)](https://github.com/kamilpajak/heisenberg/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/kamilpajak/heisenberg?style=flat-square)](https://goreportcard.com/report/github.com/kamilpajak/heisenberg)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

AI-powered test failure analysis for GitHub repositories.

Heisenberg fetches Playwright test artifacts from GitHub Actions and uses Google Gemini to diagnose root causes of test failures.

## Features

- **Playwright trace analysis** - Parses HTML reports and trace files
- **Root cause diagnosis** - AI-powered analysis with confidence scoring
- **Local dashboard** - Web UI for interactive analysis (`heisenberg serve`)
- **Progressive disclosure** - Suggests additional data sources when confidence is low

## Installation

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
│  Fetch artifacts from       │
│  GitHub Actions             │
└─────────────┬───────────────┘
              │
              ▼
┌─────────────────────────────┐
│  Detect format:             │
│  • HTML report → Playwright │
│  • blob-report → Merge      │
│  • JSON → Parse directly    │
└─────────────┬───────────────┘
              │
              ▼
┌─────────────────────────────┐
│  LLM Analysis (Gemini)      │
│  with tool calling          │
└─────────────┬───────────────┘
              │
              ▼
┌─────────────────────────────┐
│  Diagnosis with confidence  │
│  score and sensitivity      │
└─────────────────────────────┘
```

## Supported Test Frameworks

Currently optimized for:

- **Playwright** - HTML reports, blob reports, trace files

Other frameworks may work but are not fully supported yet.

## Contributing

Contributions are welcome! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

Apache 2.0 - see [LICENSE](LICENSE) for details.
