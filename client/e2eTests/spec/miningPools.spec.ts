/* eslint-disable playwright/expect-expect */
import { generateRandomText, generateRandomUsername } from "e2eTests/helpers/testDataHelper";
import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";

test.describe("Mining Pools", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test("Configure mining pool", async ({ authPage, settingsPage, settingsPoolsPage }) => {
    const invalidPoolUrl = "stratum+tcp://eu1.examplepool.com:3333";
    const validPoolUrl = "stratum+tcp://stratum.slushpool.com:3333";
    const name = generateRandomText("PoolName");
    const username = generateRandomUsername();

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
    });

    await test.step("Configure mining pool with invalid URL", async () => {
      await settingsPoolsPage.clickAddPool();
      await settingsPoolsPage.validatePoolModalOpened();
      await settingsPoolsPage.inputPoolName(name);
      await settingsPoolsPage.inputPoolUrl(invalidPoolUrl);
      await settingsPoolsPage.inputPoolUsername(username);
    });

    await test.step("Test connection - expect failure", async () => {
      await settingsPoolsPage.clickTestConnection();
      await settingsPoolsPage.validateConnectionFailed();
    });

    await test.step("Change URL to a valid one", async () => {
      await settingsPoolsPage.inputPoolUrl(validPoolUrl);
    });

    await test.step("Test connection - expect failure", async () => {
      await settingsPoolsPage.clickTestConnection();
      await settingsPoolsPage.validateConnectionSuccessful();
    });

    await test.step("Save and validate pool URL", async () => {
      await settingsPoolsPage.clickSavePool();
      await settingsPoolsPage.validatePoolEntryByUniqueName(name, validPoolUrl, username);
    });
  });
});
