// The numbered setup specs are not idempotent, so `setup-desktop` and
// `setup-mobile` cannot run against the same backend in one invocation.
// Require exactly one --project to keep a bare `npx playwright test` (or
// multiple --project flags) from scheduling both setup projects at once.
export default async function globalSetup(): Promise<void> {
  const argv = process.argv.slice(2);

  // --list is a dry-run: Playwright enumerates tests without touching the
  // backend, so the single-project invariant does not apply.
  if (argv.includes("--list")) {
    return;
  }

  const projects: string[] = [];
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if ((a === "--project" || a === "-p") && argv[i + 1]) {
      projects.push(argv[i + 1]);
      i++;
    } else if (a.startsWith("--project=")) {
      projects.push(a.slice("--project=".length));
    } else if (a.startsWith("-p=")) {
      projects.push(a.slice("-p=".length));
    }
  }

  if (projects.length !== 1) {
    throw new Error(
      [
        "",
        "Proto Fleet e2e tests require exactly one --project flag.",
        "",
        projects.length === 0
          ? "No --project was provided."
          : `Received ${projects.length} projects: ${projects.join(", ")}.`,
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
