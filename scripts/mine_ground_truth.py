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

import requests
import yaml

# --- GitHub API ---

GITHUB_API = "https://api.github.com"
SEARCH_QUERIES = [
    'in:title "fix CI" is:merged',
    'in:title "fix test" is:merged',
    'in:title "fix flaky" is:merged',
    'in:title "fix e2e" is:merged',
]


def search_fix_prs(language, date_from, token):
    """Search GitHub for merged PRs with fix-related titles."""
    headers = {"Authorization": f"token {token}", "Accept": "application/vnd.github.v3+json"}
    results = []
    for query_base in SEARCH_QUERIES:
        q = f"{query_base} language:{language} created:>{date_from}"
        resp = requests.get(
            f"{GITHUB_API}/search/issues",
            headers=headers,
            params={"q": q, "per_page": 30, "sort": "created", "order": "desc"},
        )
        resp.raise_for_status()
        for item in resp.json().get("items", []):
            if item.get("pull_request"):
                results.append(item)
    return results


def get_pr_commits_with_status(owner, repo, pr_number, token):
    """Get PR commits with their CI check conclusions."""
    headers = {"Authorization": f"token {token}", "Accept": "application/vnd.github.v3+json"}

    # Get commits
    resp = requests.get(
        f"{GITHUB_API}/repos/{owner}/{repo}/pulls/{pr_number}/commits",
        headers=headers,
        params={"per_page": 100},
    )
    resp.raise_for_status()
    commits = resp.json()

    # Get check-run conclusion for each commit
    enriched = []
    for c in commits:
        sha = c["sha"]
        status_resp = requests.get(
            f"{GITHUB_API}/repos/{owner}/{repo}/commits/{sha}/status",
            headers=headers,
        )
        status_resp.raise_for_status()
        conclusion = status_resp.json().get("state", "unknown")
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


def filter_candidate(fail, fix, diff):
    """Returns (accepted, reason) based on filtering rules."""
    if diff.get("total_lines", 0) > 50:
        return False, f"Diff too large: {diff['total_lines']} lines"
    if is_bot_author(diff.get("author", "")):
        return False, f"Bot author: {diff['author']}"
    if is_workaround(diff.get("text", "")):
        return False, "Workaround pattern detected"
    return True, "accepted"


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
    (re.compile(r"(exit code 137|OOM|OutOfMemory|killed|ENOMEM)", re.IGNORECASE), "infra"),
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
    has_config = any(INFRA_PATTERNS.search(f.get("path", "")) for f in diff_files)
    all_test = all(TEST_PATTERNS.search(f.get("path", "")) for f in diff_files) and n_files > 0

    if all_test and n_files == 1 and diff_lines < 15:
        return "easy"
    if n_files > 3 or has_config or diff_lines > 30:
        return "hard"
    return "medium"


# --- Diversity control ---


def load_wanted(path):
    """Load wanted.yaml coverage matrix."""
    with open(path) as f:
        data = yaml.safe_load(f)
    buckets = data.get("buckets", [])
    for b in buckets:
        b.setdefault("found", 0)
    return buckets


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


def save_candidate(output_dir, candidate, diff_patch="", run_metadata=None, log_excerpt=""):
    """Write candidate directory with all files."""
    case_dir = Path(output_dir) / f"{candidate['repo'].replace('/', '_')}_{candidate['run_id']}"
    case_dir.mkdir(parents=True, exist_ok=True)

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


def main():
    parser = argparse.ArgumentParser(description="Mine GitHub for ground truth candidates")
    parser.add_argument("--wanted", default="scripts/wanted.yaml", help="Coverage matrix YAML")
    parser.add_argument("--output", default="candidates/", help="Output directory")
    parser.add_argument("--languages", nargs="+", default=["typescript", "python", "go"])
    parser.add_argument("--days", type=int, default=30, help="Search window in days")
    parser.add_argument("--max-candidates", type=int, default=50)
    parser.add_argument("--dry-run", action="store_true", help="Validate config without API calls")
    args = parser.parse_args()

    token = os.environ.get("GITHUB_TOKEN")
    if not token and not args.dry_run:
        print("Error: GITHUB_TOKEN environment variable required", file=sys.stderr)
        sys.exit(1)

    # Load wanted list
    if os.path.exists(args.wanted):
        wanted = load_wanted(args.wanted)
        print(f"Loaded {len(wanted)} buckets from {args.wanted}")
    else:
        wanted = []
        print(f"No wanted file at {args.wanted}, accepting all categories")

    if args.dry_run:
        print("Dry run — config validated, no API calls.")
        return

    date_from = (datetime.now(timezone.utc) - timedelta(days=args.days)).strftime("%Y-%m-%d")
    total_candidates = 0

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
                diff_resp = requests.get(
                    f"{GITHUB_API}/repos/{owner}/{repo}/compare/{fail_commit['sha']}...{fix_commit['sha']}",
                    headers={"Authorization": f"token {token}", "Accept": "application/vnd.github.v3+json"},
                )
                diff_resp.raise_for_status()
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
            failure_type = classify_failure_type(fix_commit.get("message", ""))
            classification["failure_type"] = failure_type
            difficulty = estimate_difficulty(diff_files, total_lines)

            if not check_bucket(wanted, lang, failure_type, classification["bug_location"]):
                print(f"    Bucket full: {lang}/{failure_type}/{classification['bug_location']}")
                continue

            candidate = generate_candidate(
                repo=f"{owner}/{repo}",
                run_id=0,  # TODO: get actual failing run ID
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

            case_dir = save_candidate(args.output, candidate, diff_text)
            update_bucket(wanted, lang, failure_type, classification["bug_location"])
            total_candidates += 1
            print(f"    ✓ Saved: {case_dir}")

    print(f"\nDone. {total_candidates} candidates saved to {args.output}")


if __name__ == "__main__":
    main()
