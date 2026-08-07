#!/usr/bin/env bash
# Run the Codex Security Review locally, reusing the prompt/model from the CI workflow.
# Usage: scripts/codex-security-review-local.sh [base-ref]  (default: origin/main)
# shellcheck disable=SC2016 # single-quoted ${{ env.* }} are literal workflow tokens, not expansions
set -euo pipefail

REPO_ROOT=$(git rev-parse --show-toplevel)
cd "$REPO_ROOT"

WORKFLOW_FILE=.github/workflows/codex-security-review.yml
BASE_REF=${1:-origin/main}

if ! command -v yq >/dev/null 2>&1; then
  echo "yq not found on PATH. Activate hermit first: . ./bin/activate-hermit" >&2
  exit 1
fi

# codex must be an externally installed binary: anything under the reviewed
# checkout (Hermit shims in bin/, a branch-added PATH entry) is attacker code
# when reviewing an untrusted branch, and codex runs with the developer's login.
CODEX_BIN=$(command -v codex || true)
if [ -z "$CODEX_BIN" ]; then
  echo "codex CLI not found. Install with: brew install codex (then: codex login)" >&2
  exit 1
fi
case "$(cd "$(dirname "$CODEX_BIN")" && pwd -P)/" in
  "$REPO_ROOT"/*)
    echo "Refusing to run $CODEX_BIN: it resolves inside the reviewed checkout." >&2
    echo "Install codex outside the repository (brew install codex) so a malicious branch cannot substitute it." >&2
    exit 1
    ;;
esac

MERGE_BASE=$(git merge-base "$BASE_REF" HEAD)
HEAD_SHA=$(git rev-parse HEAD)

MODEL=$(yq '.jobs.security-review.env.CODEX_MODEL // ""' "$WORKFLOW_FILE")
EFFORT=$(yq '.jobs.security-review.env.CODEX_REASONING_EFFORT // ""' "$WORKFLOW_FILE")
if [ -z "$MODEL" ] || [ -z "$EFFORT" ]; then
  echo "Could not read CODEX_MODEL / CODEX_REASONING_EFFORT from $WORKFLOW_FILE" >&2
  exit 1
fi

# CI reviews base...head; locally we also include uncommitted tracked changes.
REVIEW_DIFF_FILE=.git/codex-review.diff
git diff --find-renames --submodule=diff --unified=40 "$MERGE_BASE" > "$REVIEW_DIFF_FILE"
if [ ! -s "$REVIEW_DIFF_FILE" ]; then
  echo "No changes found between $BASE_REF ($MERGE_BASE) and the working tree." >&2
  exit 1
fi

ORIGIN_URL=$(git remote get-url origin)
REPO_SLUG=$(echo "$ORIGIN_URL" | sed -E 's#(git@[^:]+:|https://[^/]+/)##; s#\.git$##')

# Honest provenance: with a dirty tree the diff contains content no commit has,
# so the prompt must not claim a HEAD pin nor link findings to HEAD blobs.
if git diff --quiet HEAD --; then
  REVIEW_SCOPE_SHA=$HEAD_SHA
  REVIEW_RANGE="$MERGE_BASE...$HEAD_SHA"
  REVIEW_BLOB_BASE="https://github.com/$REPO_SLUG/blob/$HEAD_SHA"
else
  REVIEW_SCOPE_SHA="$HEAD_SHA plus uncommitted working-tree changes"
  REVIEW_RANGE="$MERGE_BASE...working-tree"
  REVIEW_BLOB_BASE="file://$REPO_ROOT"
fi

# yq resolves the `prompt: |` block scalar natively (no indent arithmetic).
PROMPT=$(yq '.jobs.security-review.steps[] | select(.id == "run_codex") | .with.prompt // ""' "$WORKFLOW_FILE")
if [ -z "$PROMPT" ]; then
  echo "Could not read the run_codex prompt from $WORKFLOW_FILE" >&2
  exit 1
fi

# Fill in the ${{ env.* }} refs CI would substitute.
PROMPT=${PROMPT//'${{ env.REVIEW_DIFF_FILE }}'/$REVIEW_DIFF_FILE}
PROMPT=${PROMPT//'${{ env.REVIEW_HEAD_SHA }}'/$REVIEW_SCOPE_SHA}
PROMPT=${PROMPT//'${{ env.REVIEW_COMMIT_RANGE }}'/$REVIEW_RANGE}
PROMPT=${PROMPT//'${{ env.REVIEW_BLOB_BASE_URL }}'/$REVIEW_BLOB_BASE}
if [[ "$PROMPT" == *'${{'* ]]; then
  echo "Prompt references an unhandled \${{ ... }} expression; update this script's substitutions for $WORKFLOW_FILE" >&2
  exit 1
fi

echo "Running Codex Security Review (model=$MODEL, effort=$EFFORT) on $(wc -l < "$REVIEW_DIFF_FILE" | tr -d ' ') diff lines..."

LAST_MSG_FILE=.git/codex-review-last-message.txt
"$CODEX_BIN" exec \
  --sandbox read-only \
  --model "$MODEL" \
  -c "model_reasoning_effort=$EFFORT" \
  --output-last-message "$LAST_MSG_FILE" \
  "$PROMPT"

# Same validation as CI, then render review_markdown for terminal reading.
RESULT_JSON=.git/codex-review-result.json
sed -e '1{/^```/d;}' -e '${/^[[:space:]]*```[[:space:]]*$/d;}' "$LAST_MSG_FILE" > "$RESULT_JSON"

RISK=$(yq -p=json -oy '.overall_risk // ""' "$RESULT_JSON")
case "$RISK" in
  CRITICAL|HIGH|MEDIUM|LOW|NONE) ;;
  *) echo "Invalid overall_risk: '$RISK'" >&2; exit 1 ;;
esac
if [ "$(yq -p=json -oy '.review_markdown | type' "$RESULT_JSON")" != "!!str" ]; then
  echo "review_markdown must be a string" >&2
  exit 1
fi

REVIEW_MD=$(yq -p=json -oy '.review_markdown' "$RESULT_JSON")
if [ -z "$REVIEW_MD" ]; then
  echo "review_markdown must be a non-empty string" >&2
  exit 1
fi

printf '\nOverall risk: %s\n\n' "$RISK"
printf '%s\n' "$REVIEW_MD"
