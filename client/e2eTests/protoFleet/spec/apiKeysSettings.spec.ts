/* eslint-disable playwright/expect-expect */
import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";
import { CommonSteps } from "../helpers/commonSteps";
import { generateRandomText } from "../helpers/testDataHelper";
import { AuthPage } from "../pages/auth";
import { MinersPage } from "../pages/miners";
import { SettingsApiKeysPage } from "../pages/settingsApiKeys";

const API_KEY_PREFIX = "e2e_api_key";

test.describe("Proto Fleet - API Keys", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test.afterEach("CLEANUP: Revoke any API keys created during tests", async ({ browser }, testInfo) => {
    const isMobile = testInfo.project.use?.isMobile ?? false;
    const viewport = testInfo.project.use?.viewport;
    const context = await browser.newContext({ baseURL: testConfig.baseUrl, viewport });

    try {
      const page = await context.newPage();
      await page.goto("/");

      const authPage = new AuthPage(page, isMobile);
      const minersPage = new MinersPage(page, isMobile);
      const settingsApiKeysPage = new SettingsApiKeysPage(page, isMobile);
      const commonSteps = new CommonSteps(authPage, minersPage);

      await commonSteps.loginAsAdmin();
      await settingsApiKeysPage.navigateToApiKeysSettings();
      await settingsApiKeysPage.deleteApiKeysByPrefix(API_KEY_PREFIX);
    } finally {
      await context.close();
    }
  });

  test("Create and revoke API key", async ({ commonSteps, settingsApiKeysPage }) => {
    const apiKeyName = generateRandomText(API_KEY_PREFIX);

    await test.step("Log in as admin", async () => {
      await commonSteps.loginAsAdmin();
    });

    await test.step("Navigate to API Keys settings", async () => {
      await settingsApiKeysPage.navigateToApiKeysSettings();
      await settingsApiKeysPage.validateApiKeysPageOpened();
    });

    await test.step("Create a new API key without expiration", async () => {
      await settingsApiKeysPage.clickCreateApiKey();
      await settingsApiKeysPage.inputApiKeyName(apiKeyName);
      await settingsApiKeysPage.clickCreateInModal();
      await settingsApiKeysPage.validateApiKeyCreated();
      await settingsApiKeysPage.clickDone();
    });

    await test.step("Validate the API key appears in the list", async () => {
      await settingsApiKeysPage.validateApiKeyVisible(apiKeyName);
      await settingsApiKeysPage.validateApiKeyHasNoExpiration(apiKeyName);
    });

    await test.step("Revoke the API key", async () => {
      await settingsApiKeysPage.clickRevokeApiKey(apiKeyName);
      await settingsApiKeysPage.confirmRevokeApiKey();
      await settingsApiKeysPage.validateApiKeyNotVisible(apiKeyName);
    });
  });

  test("Expiration validation", async ({ commonSteps, settingsApiKeysPage }) => {
    const apiKeyName = generateRandomText(API_KEY_PREFIX);
    const today = new Date();
    const futureDate = new Date();
    futureDate.setDate(futureDate.getDate() + 2);

    await test.step("Log in as admin", async () => {
      await commonSteps.loginAsAdmin();
    });

    await test.step("Navigate to API Keys settings", async () => {
      await settingsApiKeysPage.navigateToApiKeysSettings();
      await settingsApiKeysPage.validateApiKeysPageOpened();
    });

    await test.step("Validate key name is required", async () => {
      await settingsApiKeysPage.clickCreateApiKey();
      await settingsApiKeysPage.clickCreateInModal();
      await settingsApiKeysPage.validateApiKeyNameRequired();
    });

    await test.step("Validate today cannot be selected as an expiration date", async () => {
      await settingsApiKeysPage.openExpirationDatePicker();
      await settingsApiKeysPage.validateExpirationDayDisabled(today.getDate());
    });

    await test.step("Create a new API key with a future expiration date", async () => {
      await settingsApiKeysPage.selectExpirationDate(futureDate);
      await settingsApiKeysPage.inputApiKeyName(apiKeyName);
      await settingsApiKeysPage.clickCreateInModal();
      await settingsApiKeysPage.validateApiKeyCreated();
      await settingsApiKeysPage.clickDone();
    });

    await test.step("Validate the API key expiration is saved", async () => {
      await settingsApiKeysPage.validateApiKeyVisible(apiKeyName);
      await settingsApiKeysPage.validateApiKeyHasExpiration(apiKeyName);
    });
  });
});
