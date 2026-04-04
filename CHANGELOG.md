# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.6.0] - 2026-04-04

### Added

- **Dynamic pattern recognition (Phase 2)** — vector similarity search for historical failure patterns using pgvector in the SaaS tier (`ee/`)
  - Gemini embedding client generates 768-dim vectors from RCA text
  - `rca_embeddings` table with HNSW cosine index for fast nearest-neighbor search
  - `DynamicMatcher` and `CompositeMatcher` combining static + dynamic patterns
  - Auto-embedding on analysis creation (async, non-blocking)
  - `GET /analyses/{id}/similar` — find historically similar failures
  - `GET /patterns/search?q=...` — free-text semantic search
- **Confidence calibration** — post-LLM pipeline reduces false positives when root cause is external
  - Rule 1: production bug claimed but no diff-fault intersection → cap 39
  - Rule 2: >50% blast radius + code blame → cap 39
  - Rule 3: uniform network errors blamed on code → cap 49
  - Rule 4: LLM uncertain about bug location → cap 49
  - Rule 5: evidence mentions HTTP/API errors but classified as code → cap 49
  - `original_confidence` and `calibration_reason` in JSON output
  - System prompt now requires considering external root causes
- **Run status validation** — prevents analyzing in-progress CI runs that produce misleading diagnoses
  - Hybrid approach: allows in-progress runs with completed failed jobs (Azure manual approval, GitHub matrix)
  - Normalized `ci.Status*` and `ci.Conclusion*` constants for CI-agnostic design
  - GitHub `timed_out` conclusion mapped to failure
- **Eval framework improvements**
  - Multi-provider eval support (GitHub + Azure)
  - Redesigned ground truth model: separates actual cause from expected output, confidence ranges, partial matching
  - Ground truth mining script (`scripts/mine_ground_truth.py`) — mines GitHub for fail→pass transitions with LLM verification

### Changed

- Docker images switched to `pgvector/pgvector:pg16` (docker-compose, CI, testutil)
- `CalibrationSignals` exported with JSON tags for future data-driven calibration
- Blast radius warning injected into cluster context when >25% of jobs share same error

### Fixed

- Similar RCAs sorted by similarity before truncation
- `bgTasks.Wait()` in server shutdown for graceful embedding completion
- Silent error swallowing in `DynamicMatcher` and `collectSimilar` — now logged
- JSON `null` vs `[]` inconsistency in similar analyses response
- `TestDeleteOldAnalyses` handles shared CI database

## [0.5.0] - 2026-04-02

### Added

- **Pattern recognition** — identifies known failure patterns in RCA output (12 curated patterns covering Playwright timeouts, assertion mismatches, DB connection issues, dependency conflicts, flaky tests, CI resource exhaustion, and more)
- `matched_patterns` field in JSON output (`omitempty` — backward compatible)
- Inline "Known Pattern" display in CLI after remediation section
- `PatternMatcher` interface for future SaaS tier (pgvector in Phase 2)

### Changed

- Repository restructured: commercial code moved to `ee/` directory (BSL-1.1), OSS code in `cmd/cli/`, `pkg/`, `internal/` (Apache-2.0)
- License Boundary CI check blocks PRs if OSS code imports from `ee/`
- Dockerfile.api updated to Go 1.25

## [0.4.0] - 2026-04-01

### Added

- `analyze` subcommand — explicit entry point for test failure analysis (`heisenberg analyze owner/repo`)
- Azure DevOps flags: `--azure-org`, `--azure-project`, `--azure-test-repo` (replaces `--org`, `--project`, `--test-repo`)
- Short flag aliases: `-f` (format), `-r` (run)
- Cross-repository file access for Azure DevOps multi-repo pipelines (`--azure-test-repo`)
- `--debug` flag for full agent conversation trace

### Changed

- Bare `heisenberg` now shows help with subcommand list instead of erroring
- Global flags (`--verbose`, `--format`, `--debug`) available to all subcommands
- Output format resolved before target resolution — piped errors now correctly produce JSON

### Deprecated

- `--org`, `--project`, `--test-repo` flags (use `--azure-org`, `--azure-project`, `--azure-test-repo`)
- Running analysis on root command (`heisenberg owner/repo`) — use `heisenberg analyze owner/repo`

## [0.3.1] - 2026-03-30

### Fixed

- GitHub Action: pass token via input default instead of `runs.env` (fixes `github.token` context error)
- GitHub Action: move `# NOSONAR` comment off `FROM` line in Dockerfile (fixes Docker parse error)
- GitHub Action: update Go builder image from 1.24 to 1.25 to match go.mod
- GitHub Action: improve entrypoint error handling — non-zero exit no longer silently swallowed
- GitHub Action: use `--format json` instead of deprecated `--json` flag

### Added

- `fix_confidence` field in RCA output (high/medium/low) — signals how actionable the remediation is
- System prompt rules for implementation-aware remediation — model inspects source before suggesting mocks

### Changed

- GitHub Action: renamed from "Heisenberg" to "Heisenberg CI Failure Analysis" for Marketplace uniqueness
- README: GitHub Action example includes `run-id` and `permissions` for `workflow_run` pattern

## [0.3.0] - 2026-03-30

### Added

- Hybrid failure clustering for large CI runs (>6 failed jobs) — pre-clusters by error signature, runs parallel per-cluster LLM analysis (#26)
- Multi-RCA output — `analyses[]` array with per-failure structured diagnosis (#25)
- Bug location classification — production, infrastructure, test, or unknown — with confidence level (#24)
- `heisenberg doctor` command — pre-flight check for tokens, network, Playwright, and config (#28)
- Config file support — optional YAML config at OS-appropriate path with precedence: flag > env > config > default (#28)
- `schema_version` field in JSON output (v1) for forward compatibility (#28)
- Structured JSON error output when `--format json` is active — CI pipelines always get parseable output (#28)
- `--from-env` flag — read `GITHUB_REPOSITORY` and `GITHUB_RUN_ID` from environment for zero-config CI mode (#27)
- `--format human|json` flag — replaces `--json`, auto-detects TTY (#27)
- `--run <URL>` flag — paste a GitHub Actions run URL directly (#27)
- `--model` flag — override Gemini model (env: `HEISENBERG_MODEL`) (#25)
- Run header with branch, event, status metadata in CLI output (#27)
- Cluster cards with colored bug location tags (#27)
- Compact progress mode for cleaner CLI output (#23)
- Agent loop circuit breaker — prevents repetitive tool calls with dynamic tool removal (#22)
- Eval framework with ground truth scoring and snapshot comparison tooling (#25)
- CLI-to-API persistence with API key authentication (`hsb_` prefix, SHA-256 hashed) (#18)
- SaaS infrastructure under BSL 1.1 — Kinde auth, Stripe billing, PostgreSQL, SvelteKit dashboard (#15)
- Testcontainers for integration tests (#17)

### Fixed

- `roundHours(0, 45)` returning "1h" instead of "45m" (#28)
- Missing `exitUsage` label in exit code map (#28)
- Agent loop guardrails for large repos — prevent infinite iteration (#19, #21)
- IDOR, RBAC, and duplicate detection in API key management (#18)

### Changed

- CLI output completely redesigned with structured RCA rendering, run header, and cluster cards (#27)
- `NewClient` signatures accept explicit token/key parameters with env var fallback (#28)
- Google API key validation in `heisenberg doctor` uses `x-goog-api-key` header (#28)
- README overhauled for OSS launch — product positioning, real example output, JSON schema docs (#29)

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

[Unreleased]: https://github.com/kamilpajak/heisenberg/compare/v0.3.1...HEAD
[0.3.1]: https://github.com/kamilpajak/heisenberg/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/kamilpajak/heisenberg/compare/v0.2.2...v0.3.0
[0.2.2]: https://github.com/kamilpajak/heisenberg/compare/v0.2.1...v0.2.2
[0.2.1]: https://github.com/kamilpajak/heisenberg/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/kamilpajak/heisenberg/compare/v0.1.2...v0.2.0
[0.1.2]: https://github.com/kamilpajak/heisenberg/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/kamilpajak/heisenberg/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/kamilpajak/heisenberg/releases/tag/v0.1.0
