"""Tests for ground truth mining script."""

import pytest

from mine_ground_truth import (
    find_fail_pass_transition,
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
            fail=_commit("a", "failure"),
            fix=_commit("b", "success"),
            diff={"total_lines": 100, "text": "", "author": "human"},
        )
        assert not accepted
        assert "lines" in reason.lower()

    def test_rejects_bot(self):
        accepted, reason = filter_candidate(
            fail=_commit("a", "failure"),
            fix=_commit("b", "success"),
            diff={"total_lines": 10, "text": "", "author": "dependabot[bot]"},
        )
        assert not accepted
        assert "bot" in reason.lower()

    def test_rejects_workaround(self):
        accepted, reason = filter_candidate(
            fail=_commit("a", "failure"),
            fix=_commit("b", "success"),
            diff={"total_lines": 10, "text": "+ time.sleep(5)", "author": "human"},
        )
        assert not accepted
        assert "workaround" in reason.lower()

    def test_accepts_clean(self):
        accepted, _ = filter_candidate(
            fail=_commit("a", "failure"),
            fix=_commit("b", "success"),
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


class TestEstimateDifficulty:
    def test_easy(self):
        files = [{"path": "tests/test_foo.py"}]
        assert estimate_difficulty(files, 10) == "easy"

    def test_medium(self):
        files = [{"path": "src/foo.ts"}, {"path": "tests/foo.test.ts"}]
        assert estimate_difficulty(files, 20) == "medium"

    def test_hard(self):
        files = [
            {"path": "src/a.ts"},
            {"path": "src/b.ts"},
            {"path": "src/c.ts"},
            {"path": ".github/workflows/ci.yml"},
        ]
        assert estimate_difficulty(files, 50) == "hard"


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
