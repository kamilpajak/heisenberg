# Heisenberg

AI Root Cause Analysis for Flaky Tests.

Heisenberg analyzes test failures from GitHub Actions artifacts and uses AI to diagnose root causes.

## Installation

```bash
# From source
go install github.com/kamilpajak/heisenberg@latest

# Or build locally
make build
```

## Usage

### Analyze Test Failures

```bash
# Analyze a local Playwright report
heisenberg analyze ./playwright-report/results.json

# Analyze from GitHub repo (auto-discovers test artifacts)
heisenberg analyze owner/repo

# Use different LLM provider
heisenberg analyze ./report.json --provider openai --model gpt-4o
heisenberg analyze ./report.json --provider anthropic

# Output as JSON
heisenberg analyze ./report.json --format json
```

### Discover Compatible Repos

```bash
# Check if a specific repo has test artifacts
heisenberg discover --repo TryGhost/Ghost

# Search for repos with test artifacts
heisenberg discover --query "playwright test" --min-stars 500 --limit 20

# Output as JSON for scripting
heisenberg discover --repo owner/repo --format json
```

## Configuration

Set environment variables for API access:

```bash
# GitHub API (recommended for higher rate limits)
export GITHUB_TOKEN=ghp_...

# LLM providers (at least one required for analysis)
export GOOGLE_API_KEY=...          # For Google Gemini (default)
export OPENAI_API_KEY=...          # For OpenAI
export ANTHROPIC_API_KEY=...       # For Anthropic Claude
```

### Default Models

| Provider  | Default Model           |
|-----------|-------------------------|
| google    | gemini-2.0-flash        |
| openai    | gpt-4o-mini             |
| anthropic | claude-sonnet-4-20250514 |

## Supported Test Frameworks

- **Playwright** - JSON and Blob report formats
- **JUnit XML** (planned)
- **pytest** (planned)
- **Jest** (planned)

## How It Works

1. **Parse**: Reads test reports and extracts failure information
2. **Analyze**: Sends failure data to an LLM for root cause analysis
3. **Report**: Provides diagnosis with confidence level, evidence, and suggested fixes

### GitHub Integration

When analyzing a GitHub repo:
1. Fetches recent workflow runs
2. Discovers artifacts matching test report patterns
3. Prioritizes artifacts from failed runs
4. Downloads and parses test reports
5. Runs AI analysis on failures

## Development

```bash
# Run tests
make test

# Build binary
make build

# Run linter
make lint

# Format code
make fmt

# Run all checks
make all
```

## Project Structure

```
├── cmd/heisenberg/     # CLI commands (Cobra)
├── internal/
│   ├── analyzer/       # AI analysis logic
│   ├── discovery/      # GitHub artifact discovery
│   ├── github/         # GitHub API client
│   ├── llm/           # LLM provider implementations
│   └── parser/        # Test report parsers
├── pkg/models/        # Shared data models
└── main.go
```

## License

MIT
