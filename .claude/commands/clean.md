# Clean Code Review

Run comprehensive code quality checks on the current branch using a phased approach that maximizes parallelism while respecting dependencies.

## Scope Control

By default, `/clean` analyzes all changes in the current git branch. You can optionally scope the cleanup to specific directories by specifying the scope when invoking the command:

**How to use:**
Tell Claude: "Run `/clean` on [directory] only"

**Examples:**
- **Server only**: "Run `/clean` on server/ only"
- **Plugins only**: "Run `/clean` on plugin/ only"
- **Specific module**: "Run `/clean` on plugin/proto/ only"
- **Client only**: "Run `/clean` on client/ only"

This is useful for large PRs where you want to incrementally clean modules or when changes are isolated to one area.

## What This Command Does

This command runs a comprehensive automated code quality workflow that:
- Ensures generated code is up to date
- Validates architectural boundaries
- Fixes magic numbers, obvious comments, and code duplication
- Removes dead code and fixes linting issues
- Verifies all changes with automated tests

The workflow is designed to be safe, with review-only phases for complex refactorings (tests, architecture) and automated fixes for mechanical issues.

## Agent Execution Modes

This command uses agents in different modes depending on safety and complexity:

**Review-Only Agents** (analyze and report, no code changes):
- `architecture-validator` - Checks for abstraction layer violations and incorrect imports
- `go-test-reviewer` - Analyzes test quality, provides refactoring recommendations

**Auto-Fix Agents** (automatically apply changes):
- `magic-values-fixer` - Replaces magic numbers with named constants and stdlib constants
- `remove-obvious-comments` - Removes obvious comments
- `rule-of-three-enforcer` - Extracts repeated patterns (3+ occurrences) into helper functions
- `dead-code-remover` - Removes unused code
- `linter-fixer` - Fixes linting violations

**Optional Auto-Fix Agent**:
- `go-test-fixer` - Automatically refactors tests to focus on behavior vs implementation (use with caution)

**Why Review-Only for Architecture and Tests?**
- **Architecture**: Violations often require understanding business context and may be intentional exceptions
- **Tests**: Test refactoring is complex and can break functionality if done incorrectly

Both provide detailed reports that can be addressed manually or (for tests) with `go-test-fixer` if you're confident.

## Execution Strategy

### Phase 0 (Optional): Architecture Review (Parallel with Phase 1)
Optionally run to catch architectural issues early:

0. **architecture-validator** - **REVIEW-ONLY**: Launch the architecture-validator agent to check for abstraction layer violations (e.g., incorrect imports, cross-layer dependencies). This is optional but recommended for PRs touching plugin or domain code. The agent checks:
   - Plugin `device.go` files shouldn't import miner API types
   - Domain packages shouldn't import handlers or external APIs
   - Shared client code shouldn't import app-specific code (protoOS/protoFleet)

### Phase 0.5: Code Generation Check (Sequential, before Phase 1)
Check if proto files, migrations, or sqlc queries were modified in this branch:

0.5. **Code generation** - Check for modified files in:
   - `proto/`
   - `server/migrations/`
   - `server/sqlc/queries/`

If any exist, run `just gen` to regenerate TypeScript and Go code from protobuf definitions and database schemas. If generation succeeds, commit the changes and proceed. If it fails, halt and report the error. This ensures generated code is in sync before cleanup begins.

### Phase 1: Foundation (Parallel)
Run these three agents in parallel since they operate on independent concerns:

1. **go-test-reviewer** - Review tests for implementation coupling and provide refactoring recommendations. This is review-only by default for safety. Tests are reviewed before refactoring begins to ensure code changes won't break brittle tests.

2. **magic-values-fixer** - **AUTO-FIX**: Replace hardcoded numbers with named constants and standard library constants (math.MaxInt32, etc.). Creates constant definitions and updates error messages to reference constants.

3. **remove-obvious-comments** - **AUTO-FIX**: Remove obvious comments that explain what code does rather than why. This is purely cosmetic and won't affect refactoring.

### Phase 2: Refactoring (Sequential)
Wait for Phase 1 to complete, then run:

4. **rule-of-three-enforcer** - **AUTO-FIX**: Find repeated code patterns (3+ occurrences) and automatically extract into helper functions. Creates new helper functions and updates all call sites. Runs after magic values are fixed, ensuring extracted helpers use proper constants instead of magic numbers.

### Phase 3: Cleanup (Sequential)
Wait for Phase 2 to complete, then run:

5. **dead-code-remover** - **AUTO-FIX**: Remove unreachable, unused, or orphaned code. Runs after refactoring because extracting functions may leave dead code behind.

### Phase 4: Final Validation (Sequential)
Wait for Phase 3 to complete, then run:

6. **linter-fixer** - **AUTO-FIX**: Run linting across all Go modules and fix any issues. Runs last to catch any linting violations introduced by previous phases.

### Phase 5: Test Execution (Sequential)
Wait for Phase 4 to complete, then run:

7. **Test execution** - Run `just test` to verify all refactoring didn't break functionality.

**Important considerations:**
- If tests were already failing before `/clean` started, baseline failures are not `/clean`'s responsibility
- For Go-only changes, run `cd server && just test` and `cd plugin/proto && go test ./...` etc.
- For client-only changes, skip this phase (see "When to Skip Phases" below)
- If tests fail, report the failures with details and halt the workflow

This is the critical safety net for all automated changes.

## When to Skip Phases

**Phase 0 (Architecture)** - Skip if:
- Only refactoring existing code without adding new cross-module dependencies
- Changes are isolated to a single module with no new imports

**Phase 0.5 (Code Gen)** - Skip if:
- No files modified in `proto/`, `server/migrations/`, or `server/sqlc/queries/`

**Phase 4 (Linter) & Phase 5 (Tests)** - Skip if:
- Only client code changed (no Go files modified)
- Use client-specific linting and testing instead

**Phase 5 (Tests)** - Consider skipping if:
- Test suite is very long-running (>10 minutes)
- You plan to run tests manually after reviewing the diff
- In this case, remind the user to run tests before committing

## Instructions

Execute the phases sequentially:

- **Phase 0 (Optional)**: Launch `architecture-validator` agent (can run in parallel with Phase 1)
- **Phase 0.5**: Check if proto/migrations/queries changed. If yes, run `just gen` and commit generated code
- **Phase 1**: Launch all three agents in PARALLEL (single message with three Task calls)
  - `go-test-reviewer` (review-only) OR `go-test-fixer` (auto-fix, use with caution)
  - `magic-values-fixer` (auto-fix)
  - `remove-obvious-comments` (auto-fix)
- **Phase 2**: After Phase 1 completes, launch `rule-of-three-enforcer` (auto-fix)
- **Phase 3**: After Phase 2 completes, launch `dead-code-remover` (auto-fix)
- **Phase 4**: After Phase 3 completes, launch `linter-fixer` (auto-fix)
- **Phase 5**: After Phase 4 completes, run `just test` to verify all changes

Analyze all changes in the current git branch (or scope to specific directory if requested).

## What Gets Fixed Automatically

**Phase 0 (Optional):**
- 📋 Architecture violations identified (review report)

**Phase 0.5:**
- ✅ Generated code regenerated and in sync with proto/migrations/queries

**Phase 1:**
- ✅ Magic numbers replaced with constants (65535 → math.MaxUint16)
- ✅ Constant definitions created for repeated values
- ✅ Error messages updated to reference constants
- ✅ Obvious comments removed
- 📋 Test coupling issues identified (review report)

**Phase 2:**
- ✅ Helper functions created for repeated patterns (3+ occurrences)
- ✅ All call sites updated to use helpers
- ✅ Code duplication eliminated

**Phase 3:**
- ✅ Dead code removed

**Phase 4:**
- ✅ All linting violations fixed

**Phase 5:**
- ✅ Tests pass, verifying all changes are safe
- ✅ Code ready for PR submission

## Using go-test-fixer (Optional)

By default, Phase 1 uses `go-test-reviewer` (review-only) for safety. You have two options:

**Option 1: Review then fix manually (default)**
1. Phase 1 runs `go-test-reviewer` and provides a report
2. Review the report and manually apply recommended changes
3. This is the safest approach

**Option 2: Automatic test fixing (use with caution)**
1. Modify Phase 1 to use `go-test-fixer` instead of `go-test-reviewer`
2. The agent will automatically refactor tests to focus on behavior vs implementation
3. **Warning**: This can break tests if not carefully validated
4. Only use if you plan to review the git diff carefully before committing

**When to use go-test-fixer:**
- You're confident in the test changes
- Tests are heavily coupled to implementation details
- You want to see the full automated cleanup in one pass
- You're prepared to revert if tests break

## After `/clean` Completes

### What `/clean` Cannot Automate

After all phases complete successfully, human review is still required for:

1. **Architecture violations** - Phase 0 report (if run) identifies issues, but fixing them requires understanding business context and whether violations are intentional
2. **Data contract assumptions** - Review external API mappings for assumptions about array indices, field stability, ordering guarantees, etc.
3. **Test correctness** - If using `go-test-fixer`, verify tests still validate the right behavior
4. **Git diff review** - Always review the full diff to ensure changes align with your intent

These require domain knowledge and judgment that automation cannot replace.

### Recommended Commit Strategy

Choose the approach that best fits your PR:

**Option 1: Single "chore: apply code quality cleanup" commit**
- Best for small changes or when cleanup is the entire PR
- Example: `chore: apply code quality cleanup via /clean command`
- Command: `git add . && git commit -m "chore: apply code quality cleanup"`

**Option 2: Separate commits per phase**
- Best for large changes where reviewers want granular history
- Example commits:
  1. `chore: regenerate proto/sqlc code` (Phase 0.5)
  2. `refactor: replace magic numbers with named constants` (Phase 1)
  3. `refactor: extract repeated patterns into helpers` (Phase 2)
  4. `refactor: remove dead code` (Phase 3)
  5. `chore: fix linting violations` (Phase 4)
- Use interactive staging: `git add -p` for each phase's changes

**Option 3: Amend to feature commit**
- Best when cleanup is minor and part of a feature branch
- Run after your feature commits: `git add . && git commit --amend --no-edit`
- Warning: Only use if you haven't pushed yet or are the only developer on the branch

**Option 4: Separate cleanup PR**
- Best for large cleanup changes on existing code
- Create a separate PR titled "chore: code quality cleanup for [module]"
- Easier to review than mixing feature and cleanup changes
