# Heisenberg

AI-powered test failure analysis for GitHub repositories.

## Roadmap

Per Product Concept (Obsidian: `Personal/Heisenberg - Product Concept.md`).

1. ~~**Local Dashboard**~~ — `heisenberg serve` showing analysis results on localhost
2. ~~**SaaS Auth/Billing**~~ — Kinde + Stripe, multi-tenant (PR #15, merged 2026-03-26)
3. **Team Dashboard** — history (180 days), permalinks, trends, PR comment bot
4. ~~**Multi-RCA + Eval**~~ — analyses array, --model flag, eval framework (PR #25, merged 2026-03-29)
5. ~~**Hybrid Failure Clustering**~~ — pre-cluster in Go, per-cluster LLM calls (PR #26, merged 2026-03-29)
6. ~~**CLI Output Redesign**~~ — run header, cluster cards, --from-env, --format, --run URL (PR #27, merged 2026-03-29)
7. ~~**DX Hardening**~~ — bug fixes, schema v1, config file, `heisenberg doctor` (PR #28, 2026-03-29)
8. ~~**Pattern Recognition Phase 1**~~ — static pattern catalog (12 patterns), fingerprint + Jaccard scoring, semantic gate (v0.5.0, 2026-04-02)
9. ~~**Pattern Recognition Phase 2**~~ — pgvector in ee/, dynamic patterns from user history, "similar to analysis #X" (PR #43, merged 2026-04-02)
10. ~~**Azure Pipelines Support**~~ — Azure DevOps provider, cross-repo file access, --azure-org/--project flags (v0.4.0, 2026-04-01)
11. ~~**CLI Refactoring**~~ — `analyze` subcommand, flag namespacing, short aliases, ee/ directory structure (v0.4.0-v0.5.0)
12. **Diagnosis Breadth** — more test frameworks (Jest, pytest, Go test), more CI providers (GitLab CI, Jenkins)
13. **Kubernetes Test Analysis** — K8s CronJob/Job provider (kubectl-based), JUnit/Gradle log parsing, integration-first approach (complement Testkube/Kuberhealthy, don't replace). Target: platform engineering teams. Validate with `check-k8s-tests` as PoC.

Consider migrating `internal/github/` to [google/go-github](https://github.com/google/go-github) when adding write operations (PR comments, check runs).

## Architecture

```
heisenberg analyze owner/repo
        ↓
   Fetch artifact from GitHub
        ↓
   ┌──────────────┐
   │ JSON?  HTML? │
   └───┬──────┬───┘
       │      │
       │   HTTP server + Playwright snapshot
       └──┬───┘
          ↓
    LLM analysis (Gemini)
          ↓
    Print conclusion
```

## Test Repositories

Real-world repos from `heisenberg-python/tests/fixtures/real_repos.py` (artifacts have 90-day retention):

| Repo | Category | Run ID | Artifacts |
|---|---|---|---|
| apache/kafka | JUnit | 21587775445 | combined-test-catalog, check-reports |
| carbon-design-system/carbon | Playwright passing | 21585598963 | 4 sharded blob reports |
| sysadminsmedia/homebox | Playwright failing | 21575780517 | blob-report-1, html-report |
| fastapi/full-stack-fastapi-template | Playwright failing | 21587420199 | 4 blob reports + html-report |
| Sage/carbon | Playwright failing | 21586769255 | 8 blob reports + html-report |
| TryGhost/Ghost | Playwright HTML | 21588220201 | merged HTML report + blob + test-results |
| rust-lang/rust | No artifacts | 21588562498 | — |
| torvalds/linux | No artifacts | 20006236072 | — |

## Test Results (2026-02-03)

| Repo | Artifact Used | Result |
|---|---|---|
| sysadminsmedia/homebox | html-report--attempt-1 (HTML) | 4 failures in `wipe-inventory.browser.spec.ts` - timeout in beforeEach |
| fastapi/full-stack-fastapi-template | html-report--attempt-1 (HTML) | 4 failures in `auth.setup.ts:6` - authentication setup problem |
| Sage/carbon | html-report--attempt-1 (HTML) | 2 failures: `simple-select.pw.tsx:1370` (keyboard nav), `time.pw.tsx:218` (onBlur) |
| TryGhost/Ghost | playwright-report (HTML) | 1 failure in `comment-replies.test.ts` - reply to reply |
| carbon-design-system/carbon | 4 blob-reports (merged) | 0 failures, 10 skipped |
| apache/kafka | — | Not supported: "no Playwright reports found (found: combined-test-catalog, check-reports)" |

Notes:
- HTML > blob priority gives same diagnosis quality as blob > HTML (tested on Sage/carbon)
- HTML is faster (1 download vs N downloads + merge)

## Environment Variables

Both keys are stored in macOS Keychain:

- `GITHUB_TOKEN`: `security find-generic-password -s "github-token" -w`
- `GOOGLE_API_KEY`: `security find-generic-password -s "gemini-api-key" -w`

### Setup (direnv)

direnv auto-loads env vars when you `cd` into the project. One-time setup:

```bash
brew install direnv
echo 'eval "$(direnv hook zsh)"' >> ~/.zshrc  # then restart terminal
```

The `.envrc` (gitignored) pulls from Keychain:
```bash
export GITHUB_TOKEN=$(security find-generic-password -s "github-token" -w)
export GOOGLE_API_KEY=$(security find-generic-password -s "gemini-api-key" -w)
```

```bash
direnv allow   # once, after creating .envrc
./heisenberg analyze owner/repo
```

## Confidence & Progressive Disclosure

Two separate dimensions, one presentation. See Product Concept (Update 2026-02-03) for full rationale.

**Two types of uncertainty:**

| Type | Dimension | User action |
|------|-----------|-------------|
| Epistemic | Data Completeness | Provide more data (`--docker`) |
| Aleatoric | Diagnosis Confidence | Manual debug or better model |

**Key rule:** Data completeness caps confidence only when relevant (HTTP 500 → need backend logs; CSS issue → logs irrelevant).

**LLM structured output must include:**
1. `confidence` (0-100)
2. `missing_information_sensitivity` (High/Medium/Low) — triggers Progressive Disclosure when High + data missing

**CLI states:**
- Green (high confidence) — actionable diagnosis, no extra data needed
- Yellow (low confidence, missing data) — suggest `--docker` / `--ci` flags
- Red (low confidence, full data) — ambiguous failure, recommend manual debug

## Corporate Data Protection

This is a **public repo**. Private/corporate names must never appear in code, commits, PR/issue bodies, or comments.

**Automated guards:**
- **Pre-commit** (lefthook `secrets-blocklist`): scans staged diffs against `.secrets-blocklist` (gitignored, one term per line)
- **CI** (`pr-blocklist` job): scans PR title + body against `PR_BLOCKLIST` GitHub secret (comma-separated)

When referencing private orgs/projects, use generic placeholders (e.g., `contoso/example-plugin`).

## Eval Framework

**Ground truth**: 171 cases in `testdata/e2e/ground-truth/*.json` (one JSON per case).

**VCR cassettes**: `testdata/e2e/cassettes/*.json.gz` — recorded HTTP interactions (GitHub API + Gemini API). Custom `internal/vcr` package (JSON+gzip), not go-vcr.

**Running eval**:
```bash
# Smoke tier (30 tagged cases, ~2.5s)
HEISENBERG_EVAL_TIER=smoke go test ./pkg/analysis/ -tags eval -run TestEval_Suite -v

# Full eval (171 cases, ~8s)
go test ./pkg/analysis/ -tags eval -run TestEval_Suite -v

# Record missing cassettes (live API, skips existing)
HEISENBERG_EVAL_RECORD=1 go test ./pkg/analysis/ -tags eval -run TestEval_Suite -v -timeout 6h

# Live mode (no VCR)
HEISENBERG_EVAL_VCR=0 go test ./pkg/analysis/ -tags eval -run TestEval_Suite -v
```

**Current baseline**: 60.9% mean score, 98.2% category accuracy (171 cases, semantic scoring + network→infra merge).

## Artifact Formats

- **blob-report** - Playwright binary format, requires merging before analysis
- **html-report** - Interactive Playwright report, requires rendering with headless browser
- **JSON** - Structured test data, directly parseable
- **JUnit XML** - Standard test report format
