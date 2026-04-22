/**
 * Enforce explicit --project selection so only one `setup-*` project runs
 * against the backend at a time.
 *
 * The numbered setup specs (00-onboarding, 01-miningPools, 02-saveAuthState)
 * create the admin user, add miners, and configure pools against a fresh
 * backend — they are not idempotent. `setup-desktop` and `setup-mobile` each
 * perform that flow under their own viewport, and the `desktop` / `mobile`
 * target projects depend on their viewport-matched setup.
 *
 * With per-shard `--project=desktop` or `--project=mobile` invocations
 * (what CI does), only one setup project is scheduled per run and the
 * design is safe. A plain `npx playwright test` with no --project flag
 * would schedule both setup projects against the same backend; the second
 * setup to reach onboarding collides with the admin user already created
 * by the first.
 *
 * Fail loudly at startup with a message pointing at the right invocation
 * rather than letting the collision show up as a confusing mid-suite
 * failure. `--ui` and `--list` opt out: `--ui` lets the developer pick a
 * project interactively, and `--list` is discovery-only (no backend).
 */
export default async function globalSetup(): Promise<void> {
  const argv = process.argv.slice(2);
  const hasProjectFlag = argv.some(
    (arg, i) =>
      arg === "--project" ||
      arg === "-p" ||
      arg.startsWith("--project=") ||
      arg.startsWith("-p=") ||
      // handle the space-separated form (prev arg is the flag)
      (i > 0 && (argv[i - 1] === "--project" || argv[i - 1] === "-p")),
  );

  const hasUiOrListOptOut = argv.includes("--ui") || argv.includes("--list");

  if (!hasProjectFlag && !hasUiOrListOptOut) {
    throw new Error(
      [
        "",
        "Proto Fleet e2e tests require an explicit --project flag.",
        "",
        "The numbered setup specs (00-onboarding, 01-miningPools,",
        "02-saveAuthState) are not idempotent: running both setup-desktop",
        "and setup-mobile against the same backend would collide on",
        "admin onboarding. Pick a single target project:",
        "",
        "  npx playwright test --project=desktop [spec/...]",
        "  npx playwright test --project=mobile  [spec/...]",
        "",
        "The CI workflow already does this per matrix shard.",
        "",
      ].join("\n"),
    );
  }
}
