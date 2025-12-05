# End-to-End Tests

This directory contains end-to-end (E2E) tests for the ProtoFleet client application using Playwright.

## Overview

The E2E test suite validates critical user workflows and functionality across the ProtoFleet application, including authentication, miner management, pool configuration, and settings management.

## Tech Stack

- **[Playwright](https://playwright.dev/)**: Modern end-to-end testing framework
- **TypeScript**: Type-safe test development
- **Page Object Model**: Organized, maintainable test structure

## Project Structure

```
e2eTests/
├── config/              # Test configuration files
│   └── test.config.ts   # Base URL, user credentials, timeouts
├── fixtures/            # Playwright fixtures for dependency injection
│   └── pageFixtures.ts  # Page object fixtures
├── pages/               # Page Object Model implementations
│   ├── base.ts          # Base page class with common methods
│   ├── auth.ts          # Authentication page objects
│   ├── miners.ts        # Miners page objects
│   └── settings.ts      # Settings page objects
├── spec/                # Test specifications
│   ├── auth.spec.ts     # Authentication tests
│   ├── miners.spec.ts   # Miner management tests
│   ├── miningPools.spec.ts  # Pool configuration tests
│   └── teamAccounts.spec.ts # Team account tests
├── playwright-report/   # Generated test reports (gitignored)
└── playwright.config.ts # Playwright configuration
```

## Getting Started

### Prerequisites

- Node.js and npm installed
- ProtoFleet client and server running locally
- Test environment set up with virtual miners (default: 12 miners)

### Installation

Playwright is already included in the project dependencies. To install Playwright browsers:

```bash
npx playwright install
```

### Configuration

Test configuration is managed in `config/test.config.ts`:

```typescript
export const testConfig = {
  baseUrl: "http://localhost:5173", // Client application URL
  users: {
    admin: {
      username: "admin",
      password: "Pass123!",
    },
  },
  timeouts: 30000,
  expectedMinerCount: 12, // Expected number of virtual miners
};
```

Adjust these values based on your local environment.

## Running Tests

### Run all tests

```bash
npx playwright test
```

### Run specific test file

```bash
npx playwright test spec/auth.spec.ts
```

### Run tests in headed mode (see browser)

```bash
npx playwright test --headed
```

### Run tests in debug mode

```bash
npx playwright test --debug
```

### Run tests in UI mode (interactive)

```bash
npx playwright test --ui
```

## Viewing Test Reports

After running tests, view the HTML report:

```bash
npx playwright show-report
```

The report includes:

- Test results and execution times
- Screenshots (captured on test runs)
- Videos (retained on failure)
- Traces (captured on first retry)

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

### Using Fixtures

Fixtures provide automatic dependency injection for page objects:

```typescript
import { test } from "../fixtures/pageFixtures";

test("My test", async ({ authPage, minersPage }) => {
  await authPage.login();
  await minersPage.validateMiners();
});
```

### Test Structure Example

```typescript
test.describe("Feature Name", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test("should perform action", async ({ authPage, minersPage }) => {
    // Arrange
    await authPage.login();

    // Act
    await minersPage.performAction();

    // Assert
    await minersPage.validateResult();
  });
});
```

## Best Practices

### Test Organization

- Group related tests using `test.describe()`
- Use descriptive test names that explain the scenario
- Keep tests independent and idempotent
- Use `beforeEach` for common setup

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

### Error Handling

- Tests automatically capture screenshots and videos on failure
- Use traces for debugging complex failures
- Add explicit waits for dynamic content

### Code Quality

- Disable `playwright/expect-expect` ESLint rule only when page objects handle assertions
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

WIP

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
