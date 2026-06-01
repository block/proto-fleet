import type { Browser, Page, ViewportSize } from "@playwright/test";
import { testConfig } from "../config/test.config";
import { AuthPage } from "../pages/auth";

async function withAuthContext<T>(
  browser: Browser,
  {
    isMobile,
    viewport,
  }: {
    isMobile: boolean;
    viewport?: ViewportSize;
  },
  fn: (authPage: AuthPage, page: Page) => Promise<T>,
) {
  const context = await browser.newContext({ baseURL: testConfig.baseUrl, viewport });

  try {
    const page = await context.newPage();
    const authPage = new AuthPage(page, isMobile);
    return await fn(authPage, page);
  } finally {
    await context.close();
  }
}

export async function seedAdminLoginActivities(
  browser: Browser,
  {
    count,
    isMobile,
    viewport,
  }: {
    count: number;
    isMobile: boolean;
    viewport?: ViewportSize;
  },
) {
  await withAuthContext(browser, { isMobile, viewport }, async (authPage, page) => {
    for (let i = 0; i < count; i++) {
      await page.goto("/");

      if (await authPage.isAlreadyLoggedIn(1000)) {
        await authPage.logout();
        await authPage.validateRedirectedToAuth();
      }

      await authPage.inputUsername(testConfig.users.admin.username);
      await authPage.inputPassword(testConfig.users.admin.password);
      await authPage.clickLogin();
      await authPage.validateLoggedIn();
    }
  });
}

export async function recordFailedAdminLogin(
  browser: Browser,
  {
    isMobile,
    viewport,
  }: {
    isMobile: boolean;
    viewport?: ViewportSize;
  },
) {
  await withAuthContext(browser, { isMobile, viewport }, async (authPage, page) => {
    await page.goto("/");
    await authPage.inputUsername(testConfig.users.admin.username);
    await authPage.inputPassword(`${testConfig.users.admin.password}-wrong`);
    await authPage.clickLogin();
    await authPage.validateInvalidCredentials();
  });
}
