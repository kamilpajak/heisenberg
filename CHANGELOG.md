# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.2.2] - 2026-02-11

### Added

- Structured Root Cause Analysis (RCA) output for diagnoses
  - Title, failure type, code location, symptom, root cause, evidence, remediation
  - Evidence types: screenshot, trace, log, network, code
  - CLI renders formatted diagnosis with `[Log]`, `[Code]` prefixes
  - JSON output includes full RCA structure in `.rca` field

### Changed

- Increased agent loop limit from 20 to 30 iterations for complex repositories

## [0.2.1] - 2026-02-11

### Added

- Smart run selection: skip analysis when latest workflow run passed
- Prevents analyzing outdated failures that have already been fixed

### Changed

- CLI message updated from "Finding latest failed run" to "Finding run to analyze"

## [0.2.0] - 2026-02-11

### Added

- GitHub Action for automated test failure analysis
- JSON output format via `--json` flag for CI integration
- Action outputs: `diagnosis`, `confidence`, `category`
- Job Summary with analysis results

### Usage

```yaml
- uses: kamilpajak/heisenberg@v0.2.0
  with:
    google-api-key: ${{ secrets.GOOGLE_API_KEY }}
```

## [0.1.2] - 2026-02-08

### Added

- Early detection of expired artifacts with clear error message
- Soft limit warning at iteration 15 to improve analysis completion
- Duplicate tool call detection to prevent spinning

### Changed

- Increased agent loop limit from 10 to 20 iterations for thorough analysis

## [0.1.1] - 2026-02-08

### Added

- Homebrew tap support (`brew install kamilpajak/tap/heisenberg`)

### Fixed

- Windows compatibility: proper signal handling and npx.cmd detection
- UTF-8 safe string truncation in progress output
- Deterministic output ordering in tool argument display

## [0.1.0] - 2026-02-08

### Added

- Initial public release
- CLI tool for analyzing GitHub Actions test artifacts
- Playwright HTML report and blob report support
- Playwright trace file analysis
- Google Gemini integration for root cause diagnosis
- Confidence scoring with progressive disclosure
- Local web dashboard (`heisenberg serve`)
- Verbose mode for debugging LLM interactions
- Support for analyzing specific workflow runs via `--run-id`

### Architecture

- Agentic tool-calling loop for intelligent artifact analysis
- Automatic artifact format detection (HTML, blob, JSON)
- HTML report rendering via headless Playwright

[Unreleased]: https://github.com/kamilpajak/heisenberg/compare/v0.2.2...HEAD
[0.2.2]: https://github.com/kamilpajak/heisenberg/compare/v0.2.1...v0.2.2
[0.2.1]: https://github.com/kamilpajak/heisenberg/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/kamilpajak/heisenberg/compare/v0.1.2...v0.2.0
[0.1.2]: https://github.com/kamilpajak/heisenberg/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/kamilpajak/heisenberg/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/kamilpajak/heisenberg/releases/tag/v0.1.0
