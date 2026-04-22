// The numbered setup specs are not idempotent, so `setup-desktop` and
// `setup-mobile` cannot run against the same backend in one invocation.
// Require --project to keep a bare `npx playwright test` from scheduling both.
export default async function globalSetup(): Promise<void> {
  const argv = process.argv.slice(2);
  const hasProjectFlag = argv.some(
    (arg, i) =>
      arg === "--project" ||
      arg === "-p" ||
      arg.startsWith("--project=") ||
      arg.startsWith("-p=") ||
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
