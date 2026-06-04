#!/usr/bin/env bash
# Release verification for license-tool.
#
# Runs six pre-release gates and prints a plain [ok] / [FAIL] line for each step.
# Any failed gate aborts the run with a non-zero exit code. No emoji, no color.
#
# Usage: ./scripts/verify-release.sh
set -euo pipefail

REPO="KofTwentyTwo/license-tool"
TAP_REPO="KofTwentyTwo/homebrew-tap"

# Run from the repository root regardless of where the script is invoked.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${SCRIPT_DIR}/.."

pass() { echo "[ok]   $1"; }
fail() {
   echo "[FAIL] $1"
   exit 1
}

# ---------------------------------------------------------------------------
# Gate 1/6: Formatting
# ---------------------------------------------------------------------------
echo "=== Gate 1/6: gofmt ==="
unformatted="$(gofmt -l .)"
if [ -z "${unformatted}" ]; then
   pass "gofmt clean"
else
   echo "The following files are not gofmt-clean:"
   echo "${unformatted}"
   fail "gofmt reported unformatted files"
fi

# ---------------------------------------------------------------------------
# Gate 2/6: Static analysis
# ---------------------------------------------------------------------------
echo "=== Gate 2/6: static analysis ==="
if go vet ./...; then
   pass "go vet"
else
   fail "go vet reported problems"
fi
if golangci-lint run; then
   pass "golangci-lint run"
else
   fail "golangci-lint reported problems"
fi

# ---------------------------------------------------------------------------
# Gate 3/6: Tests with the race detector and coverage gate
# ---------------------------------------------------------------------------
echo "=== Gate 3/6: go test -race + coverage ==="
if go test ./... -race -coverpkg=./internal/...,./cmd/... -covermode=atomic -coverprofile=cover.out; then
   pass "go test ./... -race with coverage profile"
else
   fail "tests failed under the race detector"
fi
if go run github.com/vladopajic/go-test-coverage/v2@v2.18.8 --config=.testcoverage.yml; then
   pass "100% coverage gate"
else
   fail "coverage gate failed"
fi

# ---------------------------------------------------------------------------
# Gate 4/6: Release tooling (GoReleaser)
# ---------------------------------------------------------------------------
echo "=== Gate 4/6: goreleaser ==="
if ! command -v goreleaser >/dev/null 2>&1; then
   fail "goreleaser is not installed (see https://goreleaser.com/install/)"
fi
if goreleaser check; then
   pass "goreleaser check"
else
   fail "goreleaser check rejected the configuration"
fi
if goreleaser release --snapshot --clean; then
   pass "goreleaser release --snapshot --clean"
else
   fail "goreleaser snapshot build failed"
fi

# ---------------------------------------------------------------------------
# Gate 5/6: Release infrastructure (Homebrew tap + secrets)
# ---------------------------------------------------------------------------
echo "=== Gate 5/6: release infrastructure ==="
if ! command -v gh >/dev/null 2>&1; then
   fail "gh CLI is not installed (see https://cli.github.com/)"
fi
if gh repo view "${TAP_REPO}" >/dev/null 2>&1; then
   pass "homebrew tap ${TAP_REPO} reachable"
else
   fail "homebrew tap ${TAP_REPO} not reachable"
fi
if gh secret list --repo "${REPO}" | grep -q "HOMEBREW_TAP_TOKEN"; then
   pass "HOMEBREW_TAP_TOKEN secret present on ${REPO}"
else
   fail "HOMEBREW_TAP_TOKEN secret missing on ${REPO}"
fi

# ---------------------------------------------------------------------------
# Gate 6/6: Documentation and a clean working tree
# ---------------------------------------------------------------------------
echo "=== Gate 6/6: documentation and clean tree ==="
# A dated release section in Keep-a-Changelog format, e.g. "## [1.0.0] - 2026-06-03".
if grep -Eq '^## \[[0-9]+\.[0-9]+\.[0-9]+[^]]*\] - [0-9]{4}-[0-9]{2}-[0-9]{2}' CHANGELOG.md; then
   pass "CHANGELOG has a dated release section"
else
   fail "CHANGELOG has no dated release section (expected '## [x.y.z] - YYYY-MM-DD')"
fi
if grep -q "dry-run" README.md; then
   pass "README documents dry-run behavior"
else
   fail "README does not document dry-run behavior"
fi
if grep -q "GITHUB_TOKEN" SECURITY.md && grep -q "HOMEBREW_TAP_TOKEN" SECURITY.md; then
   pass "SECURITY documents release tokens"
else
   fail "SECURITY does not document release tokens"
fi
if [ -z "$(git status --porcelain)" ]; then
   pass "git working tree clean"
else
   echo "Uncommitted changes:"
   git status --short
   fail "git working tree is not clean"
fi

echo ""
echo "[ok]   all 6 gates passed"
