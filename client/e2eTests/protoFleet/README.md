# End-to-End Tests

This directory contains end-to-end (E2E) tests for the ProtoFleet client application using Playwright.

## Overview

The E2E test suite validates critical user workflows and functionality across the ProtoFleet application, including authentication, miner management, pool configuration, and settings management.

## Getting Started

### Prerequisites

- All dependencies installed via `npm install` in the client directory
- ProtoFleet development environment and fake miners set up (usually with `just dev`)

### Quick Start

The test configuration is already set up with default values. Simply run:

```bash
just test-e2e-fleet
```

This command will:

- Install Playwright browsers automatically if needed
- Run all tests in desktop mode
- Generate an HTML report

🔒 The default credentials are `admin` and `Pass123!`

### Available Commands

**Using justfile (recommended):**

```bash
just test-e2e-fleet              # Run all tests (desktop)
just test-e2e-fleet-ui           # Run in interactive UI mode
just test-e2e-fleet-headed       # Run with visible browser
just test-e2e-fleet-wip          # Run only tests tagged @wip
```

**Using npm scripts:**

```bash
npm run test:e2e           # Run all tests (desktop)
npm run test:e2e:ui        # Run in interactive UI mode
npm run test:e2e:headed    # Run with visible browser
```

### Test Execution Strategy

- **Pull Requests**: Run the full Fleet suite through parallel spec workers
- **Nightly Builds**: Run the full Fleet suite
- **Manual Runs**: Run the full Fleet suite by default

In CI, spec files whose names start with two digits, such as `00-onboarding.spec.ts` and `01-miningPools.spec.ts`, are treated as setup specs. They are replayed first, in filename order, before each non-setup spec worker runs its assigned spec file.

**Using Playwright directly:**

```bash
cd e2eTests
npx playwright test --project=desktop    # Desktop viewport (1920x1080)
npx playwright test --project=mobile     # Mobile viewport (393x852)
npx playwright test --headed             # See browser
npx playwright test --debug              # Debug mode
npx playwright test --ui                 # Interactive UI
npx playwright test spec/auth.spec.ts    # Run specific file
```

### Viewing Test Reports

After running tests, view the HTML report:

```bash
npx playwright show-report
```

The report includes:

- Test results and execution times
- Screenshots (captured on failure)
- Videos (retained on failure)
- Traces (captured on first retry)

### Configuration

Test configuration is in `config/test.config.ts`:

```typescript
export const testConfig = {
  baseUrl: "http://localhost:5173",
  users: {
    admin: {
      username: "admin",
      password: "Pass123!",
    },
  },
  testTimeout: 60000,
  actionTimeout: 30000,
};
```

### Desktop vs Mobile Testing

The test suite supports both desktop and mobile viewports, configured in `playwright.config.ts`:

- **Desktop**: 1920x1080 viewport (default)
- **Mobile**: 393x852 viewport (iPhone 14/15/16 Pro resolution)

Switch between projects using the `--project` flag:

```bash
npx playwright test --project=desktop
npx playwright test --project=mobile
```

## Tech Stack

- **[Playwright](https://playwright.dev/)**: Modern end-to-end testing framework
- **TypeScript**: Type-safe test development
- **Page Object Model**: Organized, maintainable test structure

## Project Structure

```
e2eTests/
├── config/                    # Test configuration files
│   └── test.config.ts         # Base URL, user credentials, timeouts
├── fixtures/                  # Playwright fixtures for dependency injection
│   └── pageFixtures.ts        # Page object and helper fixtures
├── helpers/                   # Reusable test helper classes
│   ├── commonSteps.ts         # Common test workflows (login, navigation)
│   └── testDataHelper.ts      # Test data generation utilities
├── pages/                     # Page Object Model implementations
│   ├── base.ts                # Base page class with common methods
│   ├── auth.ts                # Authentication page objects
│   ├── home.ts                # Home page objects
│   ├── miners.ts              # Miners page objects
│   ├── addMiners.ts           # Add miners page objects
│   ├── editPool.ts            # Pool editor page objects
│   ├── newPoolModal.ts        # New pool modal objects
│   ├── settings.ts            # Settings page objects
│   ├── settingsSecurity.ts    # Security settings page objects
│   ├── settingsTeam.ts        # Team settings page objects
│   └── settingsPools.ts       # Pool settings page objects
├── spec/                      # Test specifications
│   ├── 00-onboarding.spec.ts  # Initial setup and onboarding tests
│   ├── 01-miningPools.spec.ts # Pool configuration tests
│   ├── auth.spec.ts           # Authentication tests
│   ├── minersActions.spec.ts  # Miner management and actions tests
│   ├── securitySettings.spec.ts # Security settings tests
│   ├── generalSettings.spec.ts # General settings tests
│   ├── teamAccounts.spec.ts   # Team account management tests
│   └── navigation.spec.ts     # Navigation flow tests
├── playwright-report/         # Generated test reports (gitignored)
└── playwright.config.ts       # Playwright configuration
```

## Writing Tests

### Page Object Pattern

Tests use the Page Object Model pattern to encapsulate page interactions:

```typescript
// Example: pages/miners.ts
export class MinersPage extends BasePage {
  async clickSelectAllCheckbox() {
    await this.page.locator('[data-testid="select-all-checkbox"]').click();
  }

  async validateAmountOfMiners(expected: number) {
    const miners = this.page.locator('[data-testid="miner-row"]');
    await expect(miners).toHaveCount(expected);
  }
}
```

### Helper Classes

Common test workflows are encapsulated in helper classes:

```typescript
// Example: helpers/commonSteps.ts
export class CommonSteps {
  async loginAsAdmin() {
    await test.step("Login as admin", async () => {
      await this.authPage.inputUsername(testConfig.users.admin.username);
      await this.authPage.inputPassword(testConfig.users.admin.password);
      await this.authPage.clickLogin();
      await this.authPage.validateLoggedIn();
    });
  }

  async goToMinersPage() {
    await test.step("Navigate to miners page", async () => {
      await this.minersPage.navigateToMinersPage();
      await this.minersPage.waitForMinersTitle();
      await this.minersPage.waitForMinersListToLoad();
    });
  }
}
```

### Using Fixtures

Fixtures provide automatic dependency injection for page objects and helpers:

```typescript
import { test } from "../fixtures/pageFixtures";

test("My test", async ({ authPage, minersPage, commonSteps }) => {
  await commonSteps.loginAsAdmin();
  await commonSteps.goToMinersPage();
  await minersPage.validateMiners();
});
```

Available fixtures:

- `authPage` - Authentication page
- `homePage` - Home page
- `minersPage` - Miners page
- `addMinersPage` - Add miners page
- `settingsPage` - Settings page
- `settingsSecurityPage` - Security settings page
- `settingsTeamPage` - Team settings page
- `settingsPoolsPage` - Pool settings page
- `editPoolPage` - Pool editor page
- `newPoolModal` - New pool modal
- `commonSteps` - Common test workflows

### Test Structure Example

```typescript
test.describe("Feature Name", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test("should perform action", async ({ minersPage, commonSteps }) => {
    // Arrange
    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    // Act
    await test.step("Perform action", async () => {
      await minersPage.performAction();
    });

    // Assert
    await test.step("Validate result", async () => {
      await minersPage.validateResult();
    });
  });
});
```

## Best Practices

### Test Organization

- Group related tests using `test.describe()`
- Use descriptive test names that explain the scenario
- Keep tests independent and idempotent
- Use `beforeEach` for common setup
- Use `test.step()` to organize test logic into readable sections
- Leverage `commonSteps` helper for frequently used workflows
- Number test files (00-, 01-) when execution order matters

### Locator Strategy

1. **Prefer data-testid attributes**: `page.locator('[data-testid="button-name"]')`
2. **Use semantic selectors**: `page.getByRole('button', { name: 'Submit' })`
3. **Avoid brittle selectors**: Don't rely on class names or DOM structure

### Assertions

- Use Playwright's built-in assertions with auto-waiting
- Validate expected states explicitly
- Include meaningful assertion messages when needed

```typescript
await expect(element).toBeVisible();
await expect(element).toHaveText("Expected text");
await expect(page).toHaveURL(/.*\/expected-path/);
```

### API Validation

- Use `page.waitForRequest()` and `page.waitForResponse()` to validate API calls
- Verify request payloads and response status codes
- Ensure critical operations complete successfully at the network level

```typescript
const responsePromise = page.waitForResponse(
  (response) => response.url().includes("/api/reboot") && response.status() === 200,
);
await minersPage.clickRebootButton();
await responsePromise;
```

### Error Handling

- Tests automatically capture screenshots and videos on failure
- Use traces for debugging complex failures
- Add explicit waits for dynamic content
- Validate both UI state and API responses for critical operations

### Code Quality

- E2E spec files already disable `playwright/expect-expect` via the shared ESLint config when page objects handle assertions
- Keep page objects focused on single pages or components
- Reuse common functionality in `BasePage`

## Troubleshooting

### Tests fail to connect to application

- Ensure the client is running on `http://localhost:5173`
- Check that the server backend is running and accessible
- Verify virtual miners are running (if testing miner functionality)

### Timeouts

- Increase timeout in `config/test.config.ts`
- Check for slow network or server responses
- Verify selectors are correct and elements are rendered

### Browser issues

- Reinstall browsers: `npx playwright install --force`
- Check Playwright version compatibility
- Clear browser state between test runs

## CI/CD Integration

E2E tests run automatically in GitHub Actions:

- **Triggers**: Pull requests affecting e2e tests, daily at 7 AM UTC, manual dispatch
- **Matrix**: Tests run on both `desktop` and `mobile` projects
- **Environment**: Ubuntu with Docker, TimescaleDB, and 12 fake miners
- **Reporting**: HTML reports and GitHub annotations

See [`.github/workflows/protofleet-e2e-tests.yml`](/.github/workflows/protofleet-e2e-tests.yml) for full workflow configuration.

### CI Test Execution

The workflow:

1. Builds ProtoFleet client (both ProtoOS and ProtoFleet apps)
2. Starts TimescaleDB service
3. Builds and runs 12 fake miners via Docker Compose
4. Runs backend server
5. Executes Playwright tests with `--project=desktop` or `--project=mobile`
6. Uploads test reports and traces as artifacts

## Additional Resources

- [Playwright Documentation](https://playwright.dev/)
- [Playwright Best Practices](https://playwright.dev/docs/best-practices)
- [Page Object Model Pattern](https://playwright.dev/docs/pom)
- [Debugging Tests](https://playwright.dev/docs/debug)

## Contributing

When adding new tests:

1. Create appropriate page objects in `pages/`
2. Add fixtures if needed in `fixtures/`
3. Write descriptive test cases in `spec/`
4. Ensure tests pass locally before committing
5. Follow existing patterns and conventions
