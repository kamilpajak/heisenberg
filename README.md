# Heisenberg

AI-powered test failure analysis for GitHub repositories.

## Usage

```bash
heisenberg <owner/repo>           # Show analysis conclusion
heisenberg <owner/repo> --verbose # Show prompts and full response
```

## Configuration

```bash
export GITHUB_TOKEN=ghp_...    # Required for artifact download
export GOOGLE_API_KEY=...      # Required for AI analysis
```

## How It Works

1. Fetches test artifacts from GitHub Actions
2. Sends artifact content to Google Gemini for analysis
3. Returns root cause analysis (language/framework agnostic)

## Build

```bash
go build -o heisenberg .
```

## License

MIT
