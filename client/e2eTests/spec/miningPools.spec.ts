/* eslint-disable playwright/expect-expect */
import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";

test.describe("Mining Pools", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test("Configure mining pool", async ({ authPage, settingsPage, settingsPoolsPage }) => {
    const poolUrl = "stratum+tcp://eu1.examplepool.com:3333";

    await test.step("Login as admin", async () => {
      await authPage.inputUsername(testConfig.users.admin.username);
      await authPage.inputPassword(testConfig.users.admin.password);
      await authPage.clickLogin();
      await authPage.validateLoggedIn();
    });

    await test.step("Navigate to mining pools settings", async () => {
      await authPage.navigateToSettingsPage();
      await settingsPage.clickNavigateToMiningPoolsSettings();
      await settingsPoolsPage.validateMiningPoolsPageOpened();
      await settingsPoolsPage.validateMiningPoolsDescription();
    });

    await test.step("Configure mining pool", async () => {
      const defaultPoolIndex = await settingsPoolsPage.getDefaultPoolIndex();
      await settingsPoolsPage.clickAddPool(defaultPoolIndex);
      await settingsPoolsPage.validatePoolModalOpened();
      await settingsPoolsPage.inputPoolUrl(defaultPoolIndex, poolUrl);
      await settingsPoolsPage.inputPoolUsername(defaultPoolIndex, "myworker");
    });

    await test.step("Test connection", async () => {
      await settingsPoolsPage.clickTestConnection();
      await settingsPoolsPage.validateConnectionFailed();
      await settingsPoolsPage.clickDismissModal();
    });

    await test.step("Save and validate pool URL", async () => {
      await settingsPoolsPage.clickSavePool();
      await settingsPoolsPage.validatePoolUrlSaved(0, poolUrl);
    });
  });
});
