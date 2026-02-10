/* eslint-disable playwright/expect-expect */
import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";
import { generateRandomText } from "../helpers/testDataHelper";

test.describe("Mining pools", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test("Check pool errors", async ({ poolPage, commonSteps }) => {
    await commonSteps.navigateToPoolsSettings();
    await commonSteps.authenticateAsAdmin();

    await test.step("Open add-pool modal", async () => {
      await poolPage.clickAddAnotherPool();
    });

    await test.step("Validate URL required for test connection", async () => {
      await poolPage.clickTestConnection();
      await poolPage.validateUrlValidationError(1, "A Pool URL is required to connect to this pool.");
    });

    await test.step("Test connection fails for invalid pool URL", async () => {
      await poolPage.inputPoolUrl("aaa", 1);
      await poolPage.clickTestConnection();
      await poolPage.validateConnectionFailed();
      await poolPage.closePoolNotConnectedCallout();
    });

    await test.step("Validate save button enable/disable rules", async () => {
      await poolPage.validateSaveButtonDisabled();

      await poolPage.inputPoolUsername("aaa", 1);
      await poolPage.validateSaveButtonDisabled();

      await poolPage.inputPoolName("aaa", 1);
      await poolPage.validateSaveButtonEnabled();

      await poolPage.inputPoolUsername("", 1);
      await poolPage.validateSaveButtonDisabled();

      await poolPage.inputPoolUsername("aaa", 1);
      await poolPage.validateSaveButtonEnabled();
    });

    await test.step("Save invalid pool URL shows error toast", async () => {
      await poolPage.clickSave();
      await poolPage.validateToastMessage("Your changes were not saved");
    });

    await commonSteps.navigateToHome();
    await commonSteps.navigateToPoolsSettings();

    await test.step("Validate the invalid pool was not saved", async () => {
      await poolPage.validatePoolRowCount(1);
    });
  });

  test("Set up backup pools", async ({ poolPage, commonSteps }) => {
    const poolName1 = generateRandomText("PoolName1");
    const poolUsername1 = generateRandomText("PoolUsername1");
    const poolName2 = generateRandomText("PoolName2");
    const poolUsername2 = generateRandomText("PoolUsername2");

    await test.step("Validate current default pool", async () => {
      await poolPage.clickMiningPoolButton();
      await poolPage.validatePoolInfoPopoverVisible();
      await poolPage.validateTitleInPopover("Mining pool");
      await poolPage.validateExactTextInPopover("Connected");
      await poolPage.validateTextInPopover("Default Pool");
      await poolPage.validateTextInPopover(testConfig.pool.url);
      await poolPage.clickViewMiningPools();
    });

    await commonSteps.authenticateAsAdmin();

    await test.step("Add first backup pool", async () => {
      await poolPage.clickAddAnotherPool();
      await poolPage.validatePoolModalOpened();
      await poolPage.inputPoolName(poolName1, 1);
      await poolPage.inputPoolUrl(testConfig.pool.url, 1);
      await poolPage.inputPoolUsername(poolUsername1, 1);
      await poolPage.clickTestConnection();
      await poolPage.validateConnectionSuccessful();
      await poolPage.clickSave();
      await poolPage.validateModalIsClosed();
    });

    await test.step("Add second backup pool", async () => {
      await poolPage.clickAddAnotherPool();
      await poolPage.validatePoolModalOpened();
      await poolPage.inputPoolName(poolName2, 2);
      await poolPage.inputPoolUrl(testConfig.pool.url, 2);
      await poolPage.inputPoolUsername(poolUsername2, 2);
      await poolPage.clickTestConnection();
      await poolPage.validateConnectionSuccessful();
      await poolPage.clickSave();
      await poolPage.validateModalIsClosed();
    });

    await test.step("Validate all 3 pool rows exist with correct details", async () => {
      await poolPage.validatePoolRowCount(3);
      await poolPage.validatePoolRowDetails(1, poolName1, testConfig.pool.url);
      await poolPage.validatePoolRowDetails(2, poolName2, testConfig.pool.url);
    });
  });
});
