# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2024-02-08

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

[Unreleased]: https://github.com/kamilpajak/heisenberg/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/kamilpajak/heisenberg/releases/tag/v0.1.0
