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

```bash
# Analyze a local Playwright report
heisenberg analyze ./report.json

# Analyze from GitHub repo
heisenberg analyze owner/repo

# Discover repos with test artifacts
heisenberg discover --min-stars 100

# Check specific repo
heisenberg discover --repo owner/repo
```

## Configuration

Set environment variables:

```bash
export GITHUB_TOKEN=ghp_...        # GitHub API access
export GOOGLE_API_KEY=...          # For Google AI (default)
export OPENAI_API_KEY=...          # For OpenAI
export ANTHROPIC_API_KEY=...       # For Anthropic
```

## Supported Test Frameworks

- Playwright (JSON, Blob reports)
- JUnit XML (planned)
- pytest (planned)
- Jest (planned)

## Development

```bash
# Run tests
make test

# Build
make build

# Run linter
make lint

# Format code
make fmt
```

## License

MIT
