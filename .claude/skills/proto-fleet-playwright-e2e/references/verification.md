# E2E verification

Run touched-file ESLint from `client/`:

```sh
npx eslint <touched-files>
```

Run Playwright from the relevant suite directory:

```sh
cd client/e2eTests/protoFleet
npx playwright test spec/<spec>.ts --project=desktop --grep "scenario"
```

Use `client/e2eTests/protoOS` for ProtoOS. Select a project explicitly; use
mobile or additional projects when the changed behavior requires them. After
the targeted scenario passes, run the entire touched spec without `--grep`.
Root `just test-e2e-fleet` and `just test-e2e-protoos` run broader suites.

Unused E2E imports/locals can fail CI typecheck even when ESLint does not
fail. Resolve unused-symbol warnings; run `cd client && npx tsc --noEmit`
when adding imports/helpers or changing types/file structure. Rerun relevant
lint before an authorized push if edits occurred since the last lint pass.

When a check fails, inspect evidence, make a focused correction, and rerun
the affected check. If startup or auth prevented reaching the spec, report
that rather than calling it a scenario failure. Broaden only when shared
changes or remaining risks justify it. Do not refresh visual baselines as an
automatic response to mismatches; the root approval rule still applies.
