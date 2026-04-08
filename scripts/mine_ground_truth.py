#!/usr/bin/env python3
"""Mine GitHub for ground truth candidates (fail→pass CI transitions).

Searches public repos for merged PRs where CI failed then passed,
extracts the fix diff as evidence of root cause, and generates
candidate ground truth files for human review.

Usage:
    python mine_ground_truth.py --wanted wanted.yaml --output candidates/
"""

import argparse
import json
import os
import re
import sys
from datetime import datetime, timedelta, timezone
from pathlib import Path

import time

import requests
import yaml

# --- GitHub API ---

GITHUB_API = "https://api.github.com"
MAX_RETRIES = 5


def github_get(url, token, params=None):
    """Rate-limit-aware GET request for GitHub API.

    Handles:
    - 429 + Retry-After header (secondary rate limits)
    - 403 + X-RateLimit-Remaining=0 (primary rate limits)
    - Proactive 2.1s sleep for /search/ endpoints (30 req/min limit)
    - Raises HTTPError for non-rate-limit errors
    """
    headers = {"Authorization": f"token {token}", "Accept": "application/vnd.github.v3+json"}

    if "/search/" in url:
        time.sleep(2.1)

    for attempt in range(MAX_RETRIES):
        try:
            resp = requests.get(url, headers=headers, params=params, timeout=30)
        except (requests.ConnectionError, requests.Timeout) as e:
            wait = min(2 ** attempt, 30)
            print(f"  ⏳ Network error ({type(e).__name__}), retrying in {wait}s...")
            time.sleep(wait)
            continue

        if resp.status_code == 429:
            wait = int(resp.headers.get("Retry-After", 60)) + 1
            print(f"  ⏳ 429 rate limited, waiting {wait}s...")
            time.sleep(wait)
            continue

        if resp.status_code == 403 and resp.headers.get("X-RateLimit-Remaining") == "0":
            reset_epoch = int(resp.headers.get("X-RateLimit-Reset", time.time() + 60))
            wait = max(0, reset_epoch - int(time.time())) + 1
            print(f"  ⏳ Primary rate limit exhausted, waiting {wait}s...")
            time.sleep(wait)
            continue

        resp.raise_for_status()
        return resp

    # Exhausted retries
    raise requests.ConnectionError(f"Failed after {MAX_RETRIES} retries: {url}")
SEARCH_QUERIES = [
    # PR title searches
    'in:title "fix CI" is:merged',
    'in:title "fix test" is:merged',
    'in:title "fix flaky" is:merged',
    'in:title "fix e2e" is:merged',
    # Go/build-specific (Perplexity: Go ecosystem uses "fix build"/"fix lint")
    'in:title "fix build" is:merged',
    'in:title "fix lint" is:merged',
    # Broader: commit message search for repos that don't use descriptive PR titles
    'in:title "resolve test failure" is:merged',
    'in:title "fix failing" is:merged',
]


def search_fix_prs(language, date_from, token):
    """Search GitHub for merged PRs with fix-related titles."""
    results = []
    for query_base in SEARCH_QUERIES:
        q = f"{query_base} language:{language} created:>{date_from}"
        resp = github_get(
            f"{GITHUB_API}/search/issues",
            token=token,
            params={"q": q, "per_page": 30, "sort": "created", "order": "desc"},
        )
        for item in resp.json().get("items", []):
            if item.get("pull_request"):
                results.append(item)
    return results


def get_check_conclusion(owner, repo, sha, token):
    """Get the overall conclusion from check-runs API (not combined status).

    Returns 'failure' if ANY check-run failed, 'success' if all passed,
    'pending' if any are still running, 'unknown' if no checks exist.
    """
    resp = github_get(
        f"{GITHUB_API}/repos/{owner}/{repo}/commits/{sha}/check-runs",
        token=token,
        params={"per_page": 100},
    )
    check_runs = resp.json().get("check_runs", [])

    if not check_runs:
        return "unknown"

    for cr in check_runs:
        if cr.get("conclusion") == "failure":
            return "failure"

    for cr in check_runs:
        if cr.get("conclusion") is None:
            return "pending"

    return "success"


def get_failing_run_id(owner, repo, sha, token):
    """Extract the workflow run ID from the first failed check-run's details_url.

    Returns 0 if no failing check-run or URL can't be parsed.
    """
    resp = github_get(
        f"{GITHUB_API}/repos/{owner}/{repo}/commits/{sha}/check-runs",
        token=token,
        params={"per_page": 100},
    )

    for cr in resp.json().get("check_runs", []):
        if cr.get("conclusion") == "failure":
            url = cr.get("details_url", "")
            # Parse run ID from: https://github.com/owner/repo/actions/runs/12345/jobs/67890
            match = re.search(r"/actions/runs/(\d+)", url)
            if match:
                return int(match.group(1))
    return 0


def fetch_failing_log(owner, repo, run_id, token, max_bytes=50000):
    """Fetch log excerpt from the first failed job in a workflow run.

    Returns log text (last max_bytes) or empty string on failure.
    """
    if not run_id:
        return ""
    try:
        resp = github_get(
            f"{GITHUB_API}/repos/{owner}/{repo}/actions/runs/{run_id}/jobs",
            token=token,
        )
        jobs = resp.json().get("jobs", [])

        for job in jobs:
            if job.get("conclusion") == "failure":
                log_resp = github_get(
                    f"{GITHUB_API}/repos/{owner}/{repo}/actions/jobs/{job['id']}/logs",
                    token=token,
                )
                if log_resp.status_code == 200:
                    text = log_resp.text
                    return text[-max_bytes:] if len(text) > max_bytes else text
        return ""
    except (requests.HTTPError, requests.ConnectionError):
        return ""


MAX_COMMITS_PER_PR = 6  # Only check last N commits to limit API calls


def get_pr_commits_with_status(owner, repo, pr_number, token):
    """Get last N PR commits with their CI check conclusions.

    Limits to MAX_COMMITS_PER_PR most recent commits to reduce API calls.
    Most fix transitions happen in the last few commits.
    """
    resp = github_get(
        f"{GITHUB_API}/repos/{owner}/{repo}/pulls/{pr_number}/commits",
        token=token,
        params={"per_page": 100},
    )
    commits = resp.json()

    # Only check last N commits — fixes are typically near the end
    commits = commits[-MAX_COMMITS_PER_PR:]

    enriched = []
    for c in commits:
        sha = c["sha"]
        conclusion = get_check_conclusion(owner, repo, sha, token)
        enriched.append({"sha": sha, "conclusion": conclusion, "message": c["commit"]["message"]})

    return enriched


# --- Transition detection ---


def find_fail_pass_transition(commits):
    """Find last fail→pass transition between consecutive commits with different SHAs."""
    result = None
    for i in range(len(commits) - 1):
        curr = commits[i]
        nxt = commits[i + 1]
        if curr["sha"] == nxt["sha"]:
            continue
        if curr["conclusion"] == "failure" and nxt["conclusion"] == "success":
            result = (curr, nxt)
    return result


# --- Filtering ---

WORKAROUND_PATTERNS = [
    re.compile(r"\+.*@Retry", re.IGNORECASE),
    re.compile(r"\+.*try\s*\{", re.IGNORECASE),
    re.compile(r"\+.*catch\s*\(", re.IGNORECASE),
    re.compile(r"\+.*\.sleep\(", re.IGNORECASE),
    re.compile(r"\+.*time\.sleep\(", re.IGNORECASE),
    re.compile(r"\+.*@pytest\.mark\.skip", re.IGNORECASE),
    re.compile(r"\+.*\.skip\(", re.IGNORECASE),
    re.compile(r"\+.*xfail", re.IGNORECASE),
]

BOT_PATTERNS = ["dependabot", "renovate", "greenkeeper", "snyk-bot"]


def is_workaround(diff_text):
    """Detect workaround patterns in diff text."""
    return any(p.search(diff_text) for p in WORKAROUND_PATTERNS)


def is_bot_author(author):
    """Detect bot accounts."""
    if not author:
        return False
    author_lower = author.lower()
    if author_lower.endswith("[bot]"):
        return True
    return any(bot in author_lower for bot in BOT_PATTERNS)


def filter_candidate(_fail, _fix, diff):
    """Returns (accepted, reason) based on filtering rules."""
    if diff.get("total_lines", 0) > 50:
        return False, f"Diff too large: {diff['total_lines']} lines"
    if is_bot_author(diff.get("author", "")):
        return False, f"Bot author: {diff['author']}"
    if is_workaround(diff.get("text", "")):
        return False, "Workaround pattern detected"
    return True, "accepted"


# --- LLM Correlation Check ---

GEMINI_API = "https://generativelanguage.googleapis.com/v1beta"
GEMINI_MODEL = "gemini-2.0-flash"


def build_correlation_prompt(log_excerpt, fix_diff, pr_title):
    """Build prompt asking LLM if the fix addresses the failure."""
    return f"""You are evaluating whether a code change (fix) directly addresses a CI test failure.

## Test Failure Log
```
{log_excerpt[:2000]}
```

## Fix Diff
```
{fix_diff[:2000]}
```

## PR Title
{pr_title}

## Question
Does this code change directly address the test failure shown in the log?
Answer YES or NO on the first line, then briefly explain why.

Rules:
- YES if the diff modifies code/tests that logically relate to the failure
- NO if the diff is unrelated (e.g., CSS change for a timeout error)
- NO if the failure looks like infrastructure/flaky (network timeout, OOM) and the fix just retries or skips
"""


def parse_correlation_response(text):
    """Parse LLM response into structured result."""
    first_line = text.strip().split("\n")[0].strip().upper()
    correlated = first_line.startswith("YES")
    return {"correlated": correlated, "reason": text.strip()[:200]}


def check_correlation(log_excerpt, fix_diff, pr_title, api_key):
    """Call Gemini to check if fix correlates with failure.

    Returns {"correlated": bool, "reason": str}.
    On API error, assumes correlated (don't reject due to API issues).
    """
    prompt = build_correlation_prompt(log_excerpt, fix_diff, pr_title)

    try:
        resp = requests.post(
            f"{GEMINI_API}/models/{GEMINI_MODEL}:generateContent?key={api_key}",
            json={
                "contents": [{"parts": [{"text": prompt}]}],
                "generationConfig": {"temperature": 0, "maxOutputTokens": 200},
            },
            timeout=30,
        )
        resp.raise_for_status()
        text = resp.json()["candidates"][0]["content"]["parts"][0]["text"]
        return parse_correlation_response(text)
    except Exception as e:
        return {"correlated": True, "reason": f"API error, assuming correlated: {e}"}


# --- LLM-as-Judge (Claude) ---

CLAUDE_API = "https://api.anthropic.com/v1/messages"
CLAUDE_MODEL = "claude-sonnet-4-20250514"


def build_judge_prompt(log_excerpt, auto_label, heisenberg_label):
    """Build prompt for Claude to adjudicate between two diagnoses."""
    return f"""You are an impartial CI test failure expert.

Task: Adjudicate between two root cause analyses for this CI failure.

## CI Failure Log
```
{log_excerpt[:3000]}
```

## Option A (Auto-classifier based on file paths)
failure_type: {auto_label.get('failure_type', 'unknown')}
bug_location: {auto_label.get('bug_location', 'unknown')}

## Option B (AI analysis tool)
failure_type: {heisenberg_label.get('failure_type', 'unknown')}
bug_location: {heisenberg_label.get('bug_location', 'unknown')}
title: {heisenberg_label.get('title', '')}
root_cause: {heisenberg_label.get('root_cause', '')}

## Instructions
1. Read the CI log and understand the actual failure.
2. Compare Option A and Option B on accuracy, completeness, evidence alignment.
3. Pick the winner or declare Tie if both are equally valid.

Output ONLY a JSON object. Keep reasoning under 50 words:
{{"winner": "A" or "B" or "Tie", "correct_failure_type": "...", "correct_bug_location": "...", "reasoning": "brief"}}"""


def parse_judge_response(text):
    """Parse judge response into structured result. Handles markdown code blocks."""
    try:
        # Strip markdown code blocks if present
        cleaned = text.strip()
        if "```json" in cleaned:
            cleaned = cleaned.split("```json")[-1]
        if "```" in cleaned:
            cleaned = cleaned.split("```")[0]
        cleaned = cleaned.strip()

        # Find JSON object
        start = cleaned.index("{")
        end = cleaned.rindex("}") + 1
        return json.loads(cleaned[start:end])
    except (ValueError, json.JSONDecodeError):
        return {"winner": "error", "reasoning": f"Failed to parse: {text[:100]}"}


def judge_diagnosis(log_excerpt, auto_label, heisenberg_label, api_key, provider="claude"):
    """Call LLM to adjudicate between auto-classifier and Heisenberg diagnoses.

    provider: "claude" (Anthropic API) or "gemini" (Google API).
    """
    prompt = build_judge_prompt(log_excerpt, auto_label, heisenberg_label)

    try:
        if provider == "gemini":
            resp = requests.post(
                f"{GEMINI_API}/models/gemini-2.0-flash:generateContent?key={api_key}",
                json={
                    "contents": [{"parts": [{"text": prompt}]}],
                    "generationConfig": {"temperature": 0, "maxOutputTokens": 500},
                },
                timeout=60,
            )
            resp.raise_for_status()
            parts = resp.json()["candidates"][0]["content"]["parts"]
            # Gemini may return multiple parts (thinking + response)
            text = parts[-1]["text"]
        else:
            resp = requests.post(
                CLAUDE_API,
                headers={
                    "x-api-key": api_key,
                    "anthropic-version": "2023-06-01",
                    "content-type": "application/json",
                },
                json={
                    "model": CLAUDE_MODEL,
                    "max_tokens": 300,
                    "messages": [{"role": "user", "content": prompt}],
                },
                timeout=30,
            )
            resp.raise_for_status()
            text = resp.json()["content"][0]["text"]
        return parse_judge_response(text)
    except Exception as e:
        return {"winner": "error", "reasoning": f"API error: {e}"}


def apply_judgment(ground_truth_path, judgment):
    """Update ground truth file with judge's corrected labels."""
    with open(ground_truth_path) as f:
        gt = json.load(f)

    gt.setdefault("metadata", {})
    gt["metadata"]["judge_reasoning"] = judgment.get("reasoning", "")[:200]
    gt["metadata"]["judge_model"] = judgment.get("_model", CLAUDE_MODEL)
    gt["metadata"]["judge_winner"] = judgment.get("winner", "")

    if judgment.get("winner") == "B":
        analyses = gt.get("expected_output", {}).get("analyses", [])
        if analyses:
            if judgment.get("correct_failure_type"):
                analyses[0]["failure_type"] = judgment["correct_failure_type"]
            if judgment.get("correct_bug_location"):
                analyses[0]["bug_location"] = judgment["correct_bug_location"]

    with open(ground_truth_path, "w") as f:
        json.dump(gt, f, indent=2)


# --- Classification ---

TEST_PATTERNS = re.compile(
    r"(test[_/]|_test\.|\.test\.|\.spec\.|__tests__|tests/|fixtures/|conftest\.py)", re.IGNORECASE
)
INFRA_PATTERNS = re.compile(
    r"(\.github/|Dockerfile|docker-compose|\.yml$|\.yaml$|Makefile|Jenkinsfile|\.ci/)", re.IGNORECASE
)
PRODUCTION_PATTERNS = re.compile(
    r"(src/|pkg/|lib/|internal/|cmd/|app/|components/|pages/|api/)", re.IGNORECASE
)

FAILURE_TYPE_PATTERNS = [
    (re.compile(r"(TimeoutError|timed?\s*out|timeout\s+\d)", re.IGNORECASE), "timeout"),
    (re.compile(r"(Assertion|assert|expect\(|toBe|toEqual|assertEqual)", re.IGNORECASE), "assertion"),
    (re.compile(r"(ECONNREFUSED|connection refused|HTTP\s*[45]\d\d|502|503|504)", re.IGNORECASE), "network"),
    (re.compile(r"(exit code 137|OOM|OutOfMemory|killed|ENOMEM|ERESOLVE|ModuleNotFoundError|ImportError|npm ERR)", re.IGNORECASE), "infra"),
]


def classify_fix(diff_files):
    """Classify fix as test/production/infrastructure by file paths."""
    has_test = False
    has_production = False
    has_infra = False

    for f in diff_files:
        path = f.get("path", "")
        if TEST_PATTERNS.search(path):
            has_test = True
        if PRODUCTION_PATTERNS.search(path):
            has_production = True
        if INFRA_PATTERNS.search(path):
            has_infra = True

    # Production takes precedence over test (production bug found by test)
    if has_production:
        return {"bug_location": "production"}
    if has_infra:
        return {"bug_location": "infrastructure"}
    if has_test:
        return {"bug_location": "test"}
    return {"bug_location": "unknown"}


def classify_failure_type(log_text):
    """Classify failure type from log content via regex."""
    for pattern, failure_type in FAILURE_TYPE_PATTERNS:
        if pattern.search(log_text):
            return failure_type
    return "unknown"


def estimate_difficulty(diff_files, diff_lines):
    """Heuristic difficulty: easy/medium/hard."""
    n_files = len(diff_files)

    # Small, single-file fixes are easy regardless of file type
    if n_files <= 1 and diff_lines < 15:
        return "easy"
    if n_files > 3 or diff_lines > 40:
        return "hard"
    return "medium"


# --- Diversity control ---


def load_wanted(path):
    """Load wanted.yaml coverage matrix and exclude rules.

    Returns (buckets, excludes) tuple.
    """
    with open(path) as f:
        data = yaml.safe_load(f)
    buckets = data.get("buckets", [])
    for b in buckets:
        b.setdefault("found", 0)
    excludes = data.get("exclude", [])
    return buckets, excludes


def is_excluded(excludes, failure_type="", bug_location=""):
    """Check if a candidate should be excluded based on exclude rules."""
    for rule in excludes:
        if "failure_type" in rule and rule["failure_type"] == failure_type:
            return True
        if "bug_location" in rule and rule["bug_location"] == bug_location:
            return True
    return False


def _bucket_matches(bucket, language, failure_type, bug_location):
    """Check if a bucket matches the given attributes (supports wildcards)."""
    lang_match = bucket.get("language", "*") in ("*", language)
    ft_match = bucket.get("failure_type", "*") in ("*", failure_type)
    bl = bucket.get("bug_location")
    bl_match = bl is None or bl == "*" or bl == bug_location
    return lang_match and ft_match and bl_match


def check_bucket(wanted, language, failure_type, bug_location):
    """Returns True if any matching bucket has room, or no bucket matches (accept unknown)."""
    matched_any = False
    for b in wanted:
        if _bucket_matches(b, language, failure_type, bug_location):
            matched_any = True
            if b["found"] < b["target"]:
                return True
    return not matched_any  # no matching bucket = accept


def update_bucket(wanted, language, failure_type, bug_location):
    """Increment found count for first matching bucket."""
    for b in wanted:
        if _bucket_matches(b, language, failure_type, bug_location):
            b["found"] += 1
            return


# --- Output generation ---


def generate_candidate(repo, run_id, transition, classification, difficulty):
    """Generate candidate.json with transition metadata and ground truth schema."""
    pr_title = transition.get("pr_title", "")
    slug = repo.replace("/", "-")
    case_id = f"gh-{slug}-{run_id}"

    review_status = "pending"
    notes = ""
    if len(pr_title) < 30:
        notes = "Vague PR title — actual_cause needs human review"

    return {
        "case_id": case_id,
        "repo": repo,
        "run_id": run_id,
        "tags": [],
        "transition": transition,
        "assets": {
            "patch_file": "fix_diff.patch",
            "log_file": "log_excerpt.txt",
        },
        "ground_truth": {
            "actual_cause": pr_title,
            "observable_by_tool": True,
            "review_status": review_status,
        },
        "expected_output": {
            "category": "diagnosis",
            "confidence_min": 60,
            "confidence_max": 0,
            "analyses": [
                {
                    "failure_type": classification.get("failure_type", "unknown"),
                    "bug_location": classification.get("bug_location", "unknown"),
                }
            ],
            "allow_partial_match": True,
        },
        "metadata": {
            "validated_date": "",
            "original_model": "",
            "heuristic_difficulty": difficulty,
            "notes": notes,
        },
    }


def candidate_exists(output_dir, repo, run_id):
    """Check if a candidate directory already exists."""
    case_dir = Path(output_dir) / f"{repo.replace('/', '_')}_{run_id}"
    return case_dir.exists()


def save_candidate(output_dir, candidate, diff_patch="", run_metadata=None, log_excerpt=""):
    """Write candidate directory with all files. Returns None if already exists."""
    case_dir = Path(output_dir) / f"{candidate['repo'].replace('/', '_')}_{candidate['run_id']}"
    if case_dir.exists():
        return None
    case_dir.mkdir(parents=True)

    with open(case_dir / "candidate.json", "w") as f:
        json.dump(candidate, f, indent=2)

    with open(case_dir / "fix_diff.patch", "w") as f:
        f.write(diff_patch)

    with open(case_dir / "log_excerpt.txt", "w") as f:
        f.write(log_excerpt)

    if run_metadata:
        with open(case_dir / "failing_run.json", "w") as f:
            json.dump(run_metadata, f, indent=2)

    return str(case_dir)


# --- CLI ---


def run_recheck(candidates_dir, api_key):
    """Run LLM correlation check on existing candidates.

    Reads each candidate.json + log_excerpt.txt + fix_diff.patch.
    Skips candidates that already have llm_correlation in metadata.
    Updates candidate.json with result. Marks uncorrelated as llm_rejected.

    Returns list of results for reporting.
    """
    candidates_path = Path(candidates_dir)
    results = []

    for case_dir in sorted(candidates_path.iterdir()):
        candidate_file = case_dir / "candidate.json"
        if not case_dir.is_dir() or not candidate_file.exists():
            continue

        with open(candidate_file) as f:
            candidate = json.load(f)

        # Skip already checked
        if candidate.get("metadata", {}).get("llm_correlation"):
            continue

        # Read log and diff
        log_file = case_dir / "log_excerpt.txt"
        diff_file = case_dir / "fix_diff.patch"
        log_text = log_file.read_text() if log_file.exists() else ""
        diff_text = diff_file.read_text() if diff_file.exists() else ""

        pr_title = candidate.get("transition", {}).get("pr_title", "")

        correlation = check_correlation(
            log_excerpt=log_text or candidate.get("ground_truth", {}).get("actual_cause", ""),
            fix_diff=diff_text,
            pr_title=pr_title,
            api_key=api_key,
        )

        # Update candidate
        candidate.setdefault("metadata", {})["llm_correlation"] = correlation["reason"][:200]
        if not correlation["correlated"]:
            candidate.setdefault("ground_truth", {})["review_status"] = "llm_rejected"

        with open(candidate_file, "w") as f:
            json.dump(candidate, f, indent=2)

        results.append({"case_id": candidate.get("case_id", case_dir.name), **correlation})

    return results



def run_judge(candidates_dir, provider="gemini"):
    """Run LLM-as-judge on eval mismatches to correct ground truth."""
    import subprocess

    if provider == "claude":
        api_key = subprocess.run(
            ["security", "find-generic-password", "-s", "anthropic-api-key", "-w"],
            capture_output=True, text=True,
        ).stdout.strip()
        if not api_key:
            print("Error: anthropic-api-key not found in macOS Keychain", file=sys.stderr)
            sys.exit(1)
        judge_model = CLAUDE_MODEL
    else:
        api_key = os.environ.get("GOOGLE_API_KEY", "")
        if not api_key:
            print("Error: GOOGLE_API_KEY required for Gemini judge", file=sys.stderr)
            sys.exit(1)
        judge_model = "gemini-2.5-flash"

    gt_dir = Path("testdata/e2e/ground-truth")
    eval_log = Path("testdata/e2e/eval.jsonl")

    if not eval_log.exists():
        print("Error: eval.jsonl not found. Run TestEval_Suite first.", file=sys.stderr)
        sys.exit(1)

    # Load eval results
    eval_entries = {}
    for line in eval_log.read_text().strip().split("\n"):
        if not line:
            continue
        entry = json.loads(line)
        key = f"{entry['repo']}_{entry['run_id']}"
        eval_entries[key] = entry

    # Find mismatches (score would be 0 — no ground truth match)
    mismatches = []
    for gt_file in sorted(gt_dir.glob("*.json")):
        gt = json.loads(gt_file.read_text())
        repo = gt.get("repo", "")
        run_id = gt.get("run_id", 0)
        key = f"{repo}_{run_id}"

        # Skip if already judged
        if gt.get("metadata", {}).get("judge_winner"):
            continue

        entry = eval_entries.get(key)
        if not entry:
            continue

        # Check if any RCA matched
        exp = gt.get("expected_output", {}).get("analyses", [{}])[0]
        rcas = entry.get("rca_details", [])
        matched = False
        for rca in rcas:
            if exp.get("failure_type") and rca.get("failure_type") == exp["failure_type"]:
                matched = True
            if exp.get("bug_location") and str(rca.get("bug_location", "")) == exp["bug_location"]:
                matched = True
        if matched:
            continue  # At least partial match, skip

        # Get log excerpt from candidate
        repo_slug = repo.replace("/", "_")
        candidate_dir = Path(candidates_dir) / f"{repo_slug}_{run_id}"
        log_text = ""
        log_file = candidate_dir / "log_excerpt.txt"
        if log_file.exists():
            log_text = log_file.read_text()[:3000]

        mismatches.append({
            "gt_file": str(gt_file),
            "repo": repo,
            "run_id": run_id,
            "auto_label": exp,
            "heisenberg_label": rcas[0] if rcas else {},
            "log_excerpt": log_text,
        })

    if not mismatches:
        print("No mismatches to judge.")
        return

    print(f"\n{'=' * 60}")
    print(f"  Judging {len(mismatches)} mismatches with Claude")
    print(f"{'=' * 60}")

    winners = {"A": 0, "B": 0, "Tie": 0, "error": 0}
    for i, m in enumerate(mismatches, 1):
        print(f"\n--- [{i}/{len(mismatches)}] {m['repo']} ---")
        print(f"  Auto:       {m['auto_label'].get('failure_type','?')}/{m['auto_label'].get('bug_location','?')}")
        h = m["heisenberg_label"]
        print(f"  Heisenberg: {h.get('failure_type','?')}/{h.get('bug_location','?')} — {h.get('title','')[:50]}")

        judgment = judge_diagnosis(
            log_excerpt=m["log_excerpt"],
            auto_label=m["auto_label"],
            heisenberg_label=m["heisenberg_label"],
            api_key=api_key,
            provider=provider,
        )

        winner = judgment.get("winner", "error")
        winners[winner] = winners.get(winner, 0) + 1
        icon = {"A": "←", "B": "→", "Tie": "=", "error": "⚠"}.get(winner, "?")
        print(f"  Judge: {icon} Winner={winner} — {judgment.get('reasoning', '')[:80]}")

        apply_judgment(m["gt_file"], judgment)

    print(f"\n{'=' * 60}")
    print(f"  Results: A={winners['A']} B={winners['B']} Tie={winners['Tie']} Error={winners['error']}")
    print(f"  Ground truth updated for {winners['B']} cases (Heisenberg was correct)")
    print(f"{'=' * 60}")


def run_spot_check(candidates_dir, count):
    """Sample N random candidates and print Docker reproduction commands."""
    import random

    candidates_path = Path(candidates_dir)
    if not candidates_path.exists():
        print(f"No candidates directory at {candidates_dir}")
        return

    case_dirs = [d for d in candidates_path.iterdir() if d.is_dir() and (d / "candidate.json").exists()]
    if not case_dirs:
        print("No candidates found.")
        return

    sample = random.sample(case_dirs, min(count, len(case_dirs)))

    print(f"\n{'=' * 60}")
    print(f"  Spot-check: {len(sample)} of {len(case_dirs)} candidates")
    print(f"{'=' * 60}")

    for i, case_dir in enumerate(sample, 1):
        with open(case_dir / "candidate.json") as f:
            c = json.load(f)

        t = c.get("transition", {})
        a = c.get("expected_output", {}).get("analyses", [{}])[0]

        print(f"\n--- [{i}/{len(sample)}] {c['repo']} ---")
        print(f"  PR: {t.get('pr_title', '')[:60]}")
        print(f"  Bug: {a.get('bug_location', '?')} | {a.get('failure_type', '?')}")
        print(f"  Fix: {t.get('fix_size_lines', '?')} lines in {', '.join(t.get('files_changed', [])[:3])}")
        print(f"  PR URL: {t.get('pr_url', '')}")
        print()
        print(f"  # Reproduce locally:")
        print(f"  git clone https://github.com/{c['repo']}.git /tmp/spot-check-{i}")
        print(f"  cd /tmp/spot-check-{i}")
        print(f"  git checkout {t.get('failing_commit', '?')}")
        print(f"  # Run tests (check CI workflow for command)")
        print(f"  # Then apply fix:")
        print(f"  git checkout {t.get('fix_commit', '?')}")
        print(f"  # Run tests again — should pass")

    print(f"\n{'=' * 60}")
    print(f"If 0/{len(sample)} false positives → ~95% confidence in full corpus.")
    print(f"If 1-2/{len(sample)} false positives → ~85-90% confidence, tighten filters.")
    print(f"{'=' * 60}")


def main():
    parser = argparse.ArgumentParser(description="Mine GitHub for ground truth candidates")
    parser.add_argument("--wanted", default="scripts/wanted.yaml", help="Coverage matrix YAML")
    parser.add_argument("--output", default="candidates/", help="Output directory")
    parser.add_argument("--languages", nargs="+", default=["typescript", "python", "go"])
    parser.add_argument("--days", type=int, default=30, help="Search window in days")
    parser.add_argument("--max-candidates", type=int, default=50)
    parser.add_argument("--llm-check", action="store_true", help="Use LLM to verify fix correlates with failure")
    parser.add_argument("--spot-check", type=int, metavar="N", help="Sample N candidates and print reproduction commands")
    parser.add_argument("--recheck", action="store_true", help="Run LLM correlation check on existing candidates")
    parser.add_argument("--judge", action="store_true", help="Use Claude to adjudicate ground truth mismatches from eval.jsonl")
    parser.add_argument("--dry-run", action="store_true", help="Validate config without API calls")
    args = parser.parse_args()

    token = os.environ.get("GITHUB_TOKEN")
    if not token and not args.dry_run:
        print("Error: GITHUB_TOKEN environment variable required", file=sys.stderr)
        sys.exit(1)

    # Load wanted list
    excludes = []
    if os.path.exists(args.wanted):
        wanted, excludes = load_wanted(args.wanted)
        print(f"Loaded {len(wanted)} buckets, {len(excludes)} exclude rules from {args.wanted}")
    else:
        wanted = []
        print(f"No wanted file at {args.wanted}, accepting all categories")

    if args.dry_run:
        print("Dry run — config validated, no API calls.")
        return

    if args.spot_check:
        run_spot_check(args.output, args.spot_check)
        return

    if args.judge:
        run_judge(args.output)
        return

    if args.recheck:
        google_key = os.environ.get("GOOGLE_API_KEY", "")
        if not google_key:
            print("Error: GOOGLE_API_KEY required for --recheck", file=sys.stderr)
            sys.exit(1)
        results = run_recheck(args.output, google_key)
        correlated = sum(1 for r in results if r["correlated"])
        rejected = len(results) - correlated
        print(f"\nRecheck complete: {correlated} correlated, {rejected} rejected, {len(results)} total checked")
        for r in results:
            status = "✓" if r["correlated"] else "✗"
            print(f"  {status} {r['case_id']}: {r.get('reason', '')[:60]}")
        return

    date_from = (datetime.now(timezone.utc) - timedelta(days=args.days)).strftime("%Y-%m-%d")
    total_candidates = 0
    seen_prs = set()  # deduplicate across search queries

    for lang in args.languages:
        if total_candidates >= args.max_candidates:
            break

        print(f"\nSearching {lang} PRs since {date_from}...")
        try:
            prs = search_fix_prs(lang, date_from, token)
        except requests.HTTPError as e:
            print(f"  Search failed: {e}", file=sys.stderr)
            continue

        print(f"  Found {len(prs)} candidate PRs")

        for pr in prs:
            if total_candidates >= args.max_candidates:
                break

            pr_url = pr.get("pull_request", {}).get("html_url", "")
            if pr_url in seen_prs:
                continue
            seen_prs.add(pr_url)

            repo_url = pr.get("repository_url", "")
            parts = repo_url.replace(f"{GITHUB_API}/repos/", "").split("/")
            if len(parts) != 2:
                continue
            owner, repo = parts

            print(f"  Checking {owner}/{repo} PR#{pr['number']}: {pr['title'][:60]}...")

            try:
                commits = get_pr_commits_with_status(owner, repo, pr["number"], token)
            except requests.HTTPError:
                print(f"    Skipped: API error")
                continue

            transition = find_fail_pass_transition(commits)
            if not transition:
                print(f"    No fail→pass transition found")
                continue

            fail_commit, fix_commit = transition
            print(f"    Found transition: {fail_commit['sha'][:7]}(fail) → {fix_commit['sha'][:7]}(pass)")

            # Get diff
            try:
                diff_resp = github_get(
                    f"{GITHUB_API}/repos/{owner}/{repo}/compare/{fail_commit['sha']}...{fix_commit['sha']}",
                    token=token,
                )
                diff_data = diff_resp.json()
            except requests.HTTPError:
                print(f"    Skipped: diff API error")
                continue

            diff_files = [{"path": f["filename"]} for f in diff_data.get("files", [])]
            total_lines = sum(f.get("changes", 0) for f in diff_data.get("files", []))
            diff_text = "\n".join(f.get("patch", "") for f in diff_data.get("files", []))

            diff = {"total_lines": total_lines, "text": diff_text, "author": fix_commit.get("author", "")}

            accepted, reason = filter_candidate(fail_commit, fix_commit, diff)
            if not accepted:
                print(f"    Filtered: {reason}")
                continue

            classification = classify_fix(diff_files)

            # Get failing run ID and fetch log for failure type classification
            failing_run_id = get_failing_run_id(owner, repo, fail_commit["sha"], token)
            log_text = fetch_failing_log(owner, repo, failing_run_id, token)
            failure_type = classify_failure_type(log_text) if log_text else "unknown"
            if failure_type == "unknown":
                failure_type = classify_failure_type(fail_commit.get("message", "") + " " + fix_commit.get("message", ""))
            classification["failure_type"] = failure_type
            difficulty = estimate_difficulty(diff_files, total_lines)

            if is_excluded(excludes, failure_type=failure_type, bug_location=classification["bug_location"]):
                print(f"    Excluded: {failure_type}/{classification['bug_location']}")
                continue

            if not check_bucket(wanted, lang, failure_type, classification["bug_location"]):
                print(f"    Bucket full: {lang}/{failure_type}/{classification['bug_location']}")
                continue

            # Early existence check — before expensive LLM call
            if candidate_exists(args.output, f"{owner}/{repo}", failing_run_id):
                print(f"    ⏭ Already exists, skipping")
                continue

            # LLM correlation check (optional, requires GOOGLE_API_KEY)
            correlation = None
            if args.llm_check:
                google_key = os.environ.get("GOOGLE_API_KEY", "")
                if google_key:
                    correlation = check_correlation(
                        log_excerpt=log_text or fail_commit.get("message", ""),
                        fix_diff=diff_text,
                        pr_title=pr["title"],
                        api_key=google_key,
                    )
                    if not correlation["correlated"]:
                        print(f"    LLM rejected: {correlation['reason'][:80]}")
                        continue
                    print(f"    LLM: correlated ✓")

            candidate = generate_candidate(
                repo=f"{owner}/{repo}",
                run_id=failing_run_id,
                transition={
                    "failing_commit": fail_commit["sha"],
                    "fix_commit": fix_commit["sha"],
                    "pr_url": pr_url,
                    "pr_title": pr["title"],
                    "files_changed": [f["path"] for f in diff_files],
                    "fix_size_lines": total_lines,
                },
                classification=classification,
                difficulty=difficulty,
            )

            if correlation:
                candidate["metadata"]["llm_correlation"] = correlation["reason"][:200]

            case_dir = save_candidate(args.output, candidate, diff_text, log_excerpt=log_text)
            if case_dir is None:
                print(f"    ⏭ Already exists, skipping")
                continue
            update_bucket(wanted, lang, failure_type, classification["bug_location"])
            total_candidates += 1
            print(f"    ✓ Saved: {case_dir}")

    print(f"\nDone. {total_candidates} candidates saved to {args.output}")


if __name__ == "__main__":
    main()
