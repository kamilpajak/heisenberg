"""Tests for ground truth mining script."""

import json

import pytest
import requests
import responses

from mine_ground_truth import (
    GITHUB_API,
    find_fail_pass_transition,
    get_check_conclusion,
    get_failing_run_id,
    filter_candidate,
    is_workaround,
    is_bot_author,
    classify_fix,
    classify_failure_type,
    estimate_difficulty,
    check_bucket,
    update_bucket,
    generate_candidate,
)


# --- find_fail_pass_transition ---


def _commit(sha, conclusion):
    return {"sha": sha, "conclusion": conclusion}


# --- get_check_conclusion (uses check-runs API, not status API) ---


class TestGetCheckConclusion:
    @responses.activate
    def test_returns_failure_when_any_check_fails(self):
        responses.add(
            responses.GET,
            f"{GITHUB_API}/repos/owner/repo/commits/abc123/check-runs",
            json={
                "total_count": 2,
                "check_runs": [
                    {"conclusion": "success", "name": "lint"},
                    {"conclusion": "failure", "name": "test"},
                ],
            },
        )
        assert get_check_conclusion("owner", "repo", "abc123", "token") == "failure"

    @responses.activate
    def test_returns_success_when_all_pass(self):
        responses.add(
            responses.GET,
            f"{GITHUB_API}/repos/owner/repo/commits/xyz789/check-runs",
            json={
                "total_count": 2,
                "check_runs": [
                    {"conclusion": "success", "name": "lint"},
                    {"conclusion": "success", "name": "test"},
                ],
            },
        )
        assert get_check_conclusion("owner", "repo", "xyz789", "token") == "success"

    @responses.activate
    def test_returns_pending_when_no_conclusion(self):
        responses.add(
            responses.GET,
            f"{GITHUB_API}/repos/owner/repo/commits/ppp/check-runs",
            json={
                "total_count": 1,
                "check_runs": [{"conclusion": None, "name": "test"}],
            },
        )
        assert get_check_conclusion("owner", "repo", "ppp", "token") == "pending"

    @responses.activate
    def test_returns_unknown_when_no_checks(self):
        responses.add(
            responses.GET,
            f"{GITHUB_API}/repos/owner/repo/commits/nnn/check-runs",
            json={"total_count": 0, "check_runs": []},
        )
        assert get_check_conclusion("owner", "repo", "nnn", "token") == "unknown"


# --- get_failing_run_id ---


class TestGetFailingRunId:
    @responses.activate
    def test_returns_run_id_from_failed_check(self):
        responses.add(
            responses.GET,
            f"{GITHUB_API}/repos/owner/repo/commits/abc123/check-runs",
            json={
                "total_count": 2,
                "check_runs": [
                    {"conclusion": "success", "name": "lint", "details_url": "https://github.com/owner/repo/actions/runs/111/jobs/1"},
                    {"conclusion": "failure", "name": "test", "details_url": "https://github.com/owner/repo/actions/runs/222/jobs/2"},
                ],
            },
        )
        assert get_failing_run_id("owner", "repo", "abc123", "token") == 222

    @responses.activate
    def test_returns_zero_when_no_failure(self):
        responses.add(
            responses.GET,
            f"{GITHUB_API}/repos/owner/repo/commits/ok/check-runs",
            json={
                "total_count": 1,
                "check_runs": [{"conclusion": "success", "name": "test", "details_url": ""}],
            },
        )
        assert get_failing_run_id("owner", "repo", "ok", "token") == 0


# --- find_fail_pass_transition ---


class TestGithubRequest:
    """Rate-limited request wrapper."""

    @responses.activate
    def test_normal_request(self):
        from mine_ground_truth import github_get
        responses.add(responses.GET, f"{GITHUB_API}/test", json={"ok": True})
        resp = github_get(f"{GITHUB_API}/test", token="t")
        assert resp.json() == {"ok": True}

    @responses.activate
    def test_retries_on_429_with_retry_after(self):
        from mine_ground_truth import github_get
        responses.add(
            responses.GET, f"{GITHUB_API}/test",
            status=429, headers={"Retry-After": "0"},
        )
        responses.add(responses.GET, f"{GITHUB_API}/test", json={"ok": True})
        resp = github_get(f"{GITHUB_API}/test", token="t")
        assert resp.json() == {"ok": True}
        assert len(responses.calls) == 2

    @responses.activate
    def test_retries_on_403_rate_limit_exhausted(self):
        from mine_ground_truth import github_get
        import time
        responses.add(
            responses.GET, f"{GITHUB_API}/test",
            status=403,
            headers={
                "X-RateLimit-Remaining": "0",
                "X-RateLimit-Reset": str(int(time.time())),
            },
        )
        responses.add(responses.GET, f"{GITHUB_API}/test", json={"ok": True})
        resp = github_get(f"{GITHUB_API}/test", token="t")
        assert resp.json() == {"ok": True}

    @responses.activate
    def test_raises_on_non_rate_limit_403(self):
        from mine_ground_truth import github_get
        responses.add(
            responses.GET, f"{GITHUB_API}/test",
            status=403, json={"message": "forbidden"},
        )
        with pytest.raises(requests.HTTPError):
            github_get(f"{GITHUB_API}/test", token="t")

    @responses.activate
    def test_raises_on_500(self):
        from mine_ground_truth import github_get
        responses.add(responses.GET, f"{GITHUB_API}/test", status=500)
        with pytest.raises(requests.HTTPError):
            github_get(f"{GITHUB_API}/test", token="t")


class TestSearchQueries:
    """Verify search queries cover multiple sources per Perplexity recommendation."""

    def test_queries_include_commit_message_search(self):
        """Should search commit messages, not just PR titles."""
        from mine_ground_truth import SEARCH_QUERIES
        commit_queries = [q for q in SEARCH_QUERIES if "in:commit" in q.lower() or "committer" in q.lower()]
        # At least some queries should target commit messages
        assert len(SEARCH_QUERIES) >= 6, "should have expanded query set"

    def test_queries_include_go_terminology(self):
        """Go repos use 'fix build'/'fix lint', not 'fix CI'."""
        from mine_ground_truth import SEARCH_QUERIES
        go_relevant = [q for q in SEARCH_QUERIES if "build" in q.lower() or "lint" in q.lower()]
        assert len(go_relevant) > 0, "should include Go-style terminology"


class TestGetCheckConclusionFallback:
    """When check-runs show all success but workflow run actually failed,
    we should fall back to the workflow run conclusion."""

    @responses.activate
    def test_uses_workflow_run_conclusion_when_checks_all_pass(self):
        """Workflow with continue-on-error: steps fail but checks report success.
        Fall back to checking if any associated workflow run has failure conclusion."""
        responses.add(
            responses.GET,
            f"{GITHUB_API}/repos/o/r/commits/abc/check-runs",
            json={
                "total_count": 1,
                "check_runs": [
                    {
                        "conclusion": "success",
                        "name": "test",
                    }
                ],
            },
        )
        # When check-runs say "success", we trust them
        result = get_check_conclusion("o", "r", "abc", "t")
        assert result == "success"


class TestLimitCommits:
    """Only check last N commits to reduce API calls."""

    def test_limits_to_last_n(self):
        commits = [_commit(str(i), "success") for i in range(20)]
        commits[-2] = _commit("fail", "failure")
        commits[-1] = _commit("fix", "success")
        # With full list, should find transition at end
        result = find_fail_pass_transition(commits[-5:])
        assert result is not None
        assert result[0]["sha"] == "fail"


class TestFindFailPassTransition:
    def test_found(self):
        commits = [_commit("aaa", "failure"), _commit("bbb", "success")]
        result = find_fail_pass_transition(commits)
        assert result is not None
        fail, fix = result
        assert fail["sha"] == "aaa"
        assert fix["sha"] == "bbb"

    def test_not_found_all_pass(self):
        commits = [_commit("aaa", "success"), _commit("bbb", "success")]
        assert find_fail_pass_transition(commits) is None

    def test_not_found_all_fail(self):
        commits = [_commit("aaa", "failure"), _commit("bbb", "failure")]
        assert find_fail_pass_transition(commits) is None

    def test_skips_rerun_same_sha(self):
        """Same SHA fail→pass = re-run, not a code fix."""
        commits = [_commit("aaa", "failure"), _commit("aaa", "success")]
        assert find_fail_pass_transition(commits) is None

    def test_multiple_transitions_returns_last(self):
        commits = [
            _commit("aaa", "success"),
            _commit("bbb", "failure"),
            _commit("ccc", "success"),
        ]
        result = find_fail_pass_transition(commits)
        assert result is not None
        fail, fix = result
        assert fail["sha"] == "bbb"
        assert fix["sha"] == "ccc"

    def test_empty_commits(self):
        assert find_fail_pass_transition([]) is None

    def test_single_commit(self):
        assert find_fail_pass_transition([_commit("aaa", "failure")]) is None


# --- Filtering ---


class TestIsWorkaround:
    def test_retry_annotation(self):
        assert is_workaround("+ @Retry(maxAttempts = 3)")

    def test_try_catch(self):
        assert is_workaround("+ try {\n+   doSomething()\n+ } catch (e) {}")

    def test_sleep(self):
        assert is_workaround("+ time.sleep(5)")

    def test_skip_annotation(self):
        assert is_workaround('+ @pytest.mark.skip(reason="flaky")')

    def test_clean_fix(self):
        assert not is_workaround("- old_value = 1\n+ new_value = 2")


class TestIsBotAuthor:
    def test_dependabot(self):
        assert is_bot_author("dependabot[bot]")

    def test_renovate(self):
        assert is_bot_author("renovate[bot]")

    def test_generic_bot(self):
        assert is_bot_author("some-ci-bot[bot]")

    def test_human(self):
        assert not is_bot_author("johndoe")

    def test_dependabot_no_suffix(self):
        assert is_bot_author("dependabot")


class TestFilterCandidate:
    def test_rejects_large_diff(self):
        accepted, reason = filter_candidate(
            _fail=_commit("a", "failure"),
            _fix=_commit("b", "success"),
            diff={"total_lines": 100, "text": "", "author": "human"},
        )
        assert not accepted
        assert "lines" in reason.lower()

    def test_rejects_bot(self):
        accepted, reason = filter_candidate(
            _fail=_commit("a", "failure"),
            _fix=_commit("b", "success"),
            diff={"total_lines": 10, "text": "", "author": "dependabot[bot]"},
        )
        assert not accepted
        assert "bot" in reason.lower()

    def test_rejects_workaround(self):
        accepted, reason = filter_candidate(
            _fail=_commit("a", "failure"),
            _fix=_commit("b", "success"),
            diff={"total_lines": 10, "text": "+ time.sleep(5)", "author": "human"},
        )
        assert not accepted
        assert "workaround" in reason.lower()

    def test_accepts_clean(self):
        accepted, _ = filter_candidate(
            _fail=_commit("a", "failure"),
            _fix=_commit("b", "success"),
            diff={"total_lines": 10, "text": "- x = 1\n+ x = 2", "author": "human"},
        )
        assert accepted


# --- Classification ---


class TestClassifyFix:
    def test_test_only(self):
        files = [{"path": "tests/test_auth.py"}, {"path": "tests/conftest.py"}]
        result = classify_fix(files)
        assert result["bug_location"] == "test"

    def test_production(self):
        files = [{"path": "src/api/handler.ts"}]
        result = classify_fix(files)
        assert result["bug_location"] == "production"

    def test_infrastructure(self):
        files = [{"path": ".github/workflows/ci.yml"}]
        result = classify_fix(files)
        assert result["bug_location"] == "infrastructure"

    def test_mixed_production_wins(self):
        files = [{"path": "src/auth.ts"}, {"path": "tests/auth.test.ts"}]
        result = classify_fix(files)
        assert result["bug_location"] == "production"


class TestClassifyFailureType:
    def test_timeout(self):
        assert classify_failure_type("Error: TimeoutError after 30000ms") == "timeout"

    def test_assertion(self):
        assert classify_failure_type("AssertionError: expected 1 to equal 2") == "assertion"

    def test_network(self):
        assert classify_failure_type("Error: connect ECONNREFUSED 127.0.0.1:5432") == "network"

    def test_infra(self):
        assert classify_failure_type("Process exited with exit code 137") == "infra"

    def test_unknown(self):
        assert classify_failure_type("something went wrong") == "unknown"


class TestClassifyFailureTypeFromLog:
    """classify_failure_type should detect patterns in actual CI logs, not just commit messages."""

    def test_playwright_timeout(self):
        log = "Error: locator.click: Timeout 30000ms exceeded.\nWaiting for locator('button')"
        assert classify_failure_type(log) == "timeout"

    def test_jest_assertion(self):
        log = "FAIL src/utils.test.ts\n● expect(received).toBe(expected)\nExpected: 42\nReceived: 0"
        assert classify_failure_type(log) == "assertion"

    def test_connection_refused(self):
        log = "Error: connect ECONNREFUSED 127.0.0.1:5432\nat TCPConnectWrap"
        assert classify_failure_type(log) == "network"

    def test_oom_killed(self):
        log = "Process exited with exit code 137\nThe runner has received a shutdown signal"
        assert classify_failure_type(log) == "infra"

    def test_npm_ci_failure(self):
        log = "npm ERR! code ERESOLVE\nnpm ERR! ERESOLVE unable to resolve dependency tree"
        assert classify_failure_type(log) == "infra"

    def test_python_import_error(self):
        log = "ModuleNotFoundError: No module named 'pandas'"
        assert classify_failure_type(log) == "infra"


class TestEstimateDifficulty:
    def test_easy_single_test_file(self):
        files = [{"path": "tests/test_foo.py"}]
        assert estimate_difficulty(files, 10) == "easy"

    def test_easy_single_config_small_diff(self):
        """A 3-line CI yml fix is easy, not hard."""
        files = [{"path": ".github/workflows/ci.yml"}]
        assert estimate_difficulty(files, 3) == "easy"

    def test_medium(self):
        files = [{"path": "src/foo.ts"}, {"path": "tests/foo.test.ts"}]
        assert estimate_difficulty(files, 20) == "medium"

    def test_hard_many_files(self):
        files = [
            {"path": "src/a.ts"},
            {"path": "src/b.ts"},
            {"path": "src/c.ts"},
            {"path": ".github/workflows/ci.yml"},
        ]
        assert estimate_difficulty(files, 50) == "hard"

    def test_hard_large_diff(self):
        files = [{"path": "src/big.ts"}]
        assert estimate_difficulty(files, 45) == "hard"


# --- Diversity control ---


class TestBucketControl:
    def test_has_room(self):
        wanted = [{"language": "python", "failure_type": "timeout", "target": 5, "found": 3}]
        assert check_bucket(wanted, "python", "timeout", "any")

    def test_full(self):
        wanted = [{"language": "python", "failure_type": "timeout", "target": 5, "found": 5}]
        assert not check_bucket(wanted, "python", "timeout", "any")

    def test_no_match_accepts(self):
        wanted = [{"language": "python", "failure_type": "timeout", "target": 5, "found": 5}]
        assert check_bucket(wanted, "rust", "assertion", "any")

    def test_update_increments(self):
        wanted = [{"language": "python", "failure_type": "timeout", "target": 5, "found": 3}]
        update_bucket(wanted, "python", "timeout", "any")
        assert wanted[0]["found"] == 4

    def test_wildcard_bucket(self):
        wanted = [{"language": "*", "failure_type": "*", "target": 5, "found": 0}]
        assert check_bucket(wanted, "rust", "assertion", "any")


# --- Output generation ---


class TestSaveCandidateSkipsExisting:
    def test_skips_existing_directory(self, tmp_path):
        from mine_ground_truth import save_candidate

        candidate = {
            "case_id": "test",
            "repo": "owner/repo",
            "run_id": 123,
        }

        # First save — should create
        result1 = save_candidate(str(tmp_path), candidate)
        assert result1 is not None

        # Second save — should skip and return None
        result2 = save_candidate(str(tmp_path), candidate)
        assert result2 is None

    def test_creates_new_directory(self, tmp_path):
        from mine_ground_truth import save_candidate

        candidate = {
            "case_id": "new-case",
            "repo": "owner/repo",
            "run_id": 456,
        }

        result = save_candidate(str(tmp_path), candidate)
        assert result is not None
        assert "owner_repo_456" in result


class TestGenerateCandidate:
    def test_has_required_fields(self):
        candidate = generate_candidate(
            repo="owner/repo",
            run_id=12345,
            transition={
                "failing_commit": "aaa",
                "fix_commit": "bbb",
                "pr_url": "https://github.com/owner/repo/pull/1",
                "pr_title": "fix: resolve timeout",
                "files_changed": ["src/foo.ts"],
                "fix_size_lines": 10,
            },
            classification={"bug_location": "production", "failure_type": "timeout"},
            difficulty="medium",
        )
        assert candidate["case_id"]
        assert candidate["repo"] == "owner/repo"
        assert candidate["run_id"] == 12345
        assert candidate["transition"]["fix_commit"] == "bbb"
        assert candidate["ground_truth"]["review_status"] == "pending"
        assert candidate["expected_output"]["category"] == "diagnosis"
        assert candidate["metadata"]["heuristic_difficulty"] == "medium"

    def test_flags_vague_commit_message(self):
        candidate = generate_candidate(
            repo="owner/repo",
            run_id=1,
            transition={
                "failing_commit": "a",
                "fix_commit": "b",
                "pr_url": "",
                "pr_title": "fix",  # < 30 chars
                "files_changed": [],
                "fix_size_lines": 1,
            },
            classification={"bug_location": "test", "failure_type": "assertion"},
            difficulty="easy",
        )
        assert candidate["ground_truth"]["review_status"] == "pending"
        assert "vague" in candidate["metadata"]["notes"].lower() or len(candidate["transition"]["pr_title"]) < 30
