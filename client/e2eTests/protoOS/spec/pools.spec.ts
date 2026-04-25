/* eslint-disable playwright/expect-expect */
import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";
import { generateRandomText } from "../helpers/testDataHelper";

test.describe("Mining pools", () => {
  test.beforeEach(async ({ page, commonSteps }) => {
    await page.goto("/");
    await commonSteps.authenticateAsAdmin();
  });

  test("Check pool errors", async ({ poolsPage: poolsPage, commonSteps }) => {
    await commonSteps.navigateToPoolsSettings();

    await test.step("Open add-pool modal", async () => {
      await poolsPage.clickAddAnotherPool();
    });

    await test.step("Validate URL required for test connection", async () => {
      await poolsPage.clickTestConnection();
      await poolsPage.validateUrlValidationError(1, "A Pool URL is required to connect to this pool.");
    });

    await test.step("Test connection fails for invalid pool URL", async () => {
      await poolsPage.inputPoolUrl("aaa", 1);
      await poolsPage.clickTestConnection();
      await poolsPage.validateConnectionFailed();
      await poolsPage.closePoolNotConnectedCallout();
    });

    await test.step("Validate save button enable/disable rules", async () => {
      await poolsPage.validateSaveButtonDisabled();

      await poolsPage.inputPoolUsername("aaa", 1);
      await poolsPage.validateSaveButtonDisabled();

      await poolsPage.inputPoolName("aaa", 1);
      await poolsPage.validateSaveButtonEnabled();

      await poolsPage.inputPoolUsername("", 1);
      await poolsPage.validateSaveButtonEnabled();

      await poolsPage.inputPoolUsername("aaa", 1);
      await poolsPage.validateSaveButtonEnabled();
    });

    await test.step("Save invalid pool URL surfaces a scheme validation error inline", async () => {
      // Client-side scheme validation now mirrors the server CEL rule, so
      // an obviously-wrong URL fails fast on Save with an inline error
      // instead of a server-side rejection toast — same outcome, just
      // surfaced before the RPC. The modal stays open with the error
      // shown, so we close it explicitly before continuing.
      await poolsPage.clickSave();
      await poolsPage.validateUrlValidationError(
        1,
        "Pool URL must start with stratum+tcp:// (Stratum V1) or stratum2+tcp:// (Stratum V2). Plain TCP only in v1; TLS / WebSocket variants are not supported by the dispatch path.",
      );
      await poolsPage.closePoolModal();
    });

    await commonSteps.navigateToHome();
    await commonSteps.navigateToPoolsSettings();

    await test.step("Validate the invalid pool was not saved", async () => {
      await poolsPage.validatePoolRowCount(1);
    });
  });

  test("Set up backup pools", async ({ poolsPage: poolsPage }) => {
    const poolName1 = generateRandomText("PoolName1");
    const poolUsername1 = generateRandomText("PoolUsername1");
    const poolName2 = generateRandomText("PoolName2");
    const poolUsername2 = generateRandomText("PoolUsername2");

    await test.step("Validate current default pool", async () => {
      await poolsPage.clickMiningPoolButton();
      await poolsPage.validatePoolInfoPopoverVisible();
      await poolsPage.validateTitleInPopover("Mining pool");
      await poolsPage.validateExactTextInPopover("Connected");
      await poolsPage.validateTextInPopover("Default Pool");
      await poolsPage.validateTextInPopover(testConfig.pool.url);
      await poolsPage.clickViewMiningPools();
    });

    await test.step("Add first backup pool", async () => {
      await poolsPage.clickAddAnotherPool();
      await poolsPage.validatePoolModalOpened();
      await poolsPage.inputPoolName(poolName1, 1);
      await poolsPage.inputPoolUrl(testConfig.pool.url, 1);
      await poolsPage.inputPoolUsername(poolUsername1, 1);
      await poolsPage.clickTestConnection();
      await poolsPage.validateConnectionSuccessful();
      await poolsPage.clickSave();
      await poolsPage.validateModalIsClosed();
    });

    await test.step("Add second backup pool", async () => {
      await poolsPage.clickAddAnotherPool();
      await poolsPage.validatePoolModalOpened();
      await poolsPage.inputPoolName(poolName2, 2);
      await poolsPage.inputPoolUrl(testConfig.pool.url, 2);
      await poolsPage.inputPoolUsername(poolUsername2, 2);
      await poolsPage.clickTestConnection();
      await poolsPage.validateConnectionSuccessful();
      await poolsPage.clickSave();
      await poolsPage.validateModalIsClosed();
    });

    await test.step("Validate all 3 pool rows exist with correct details", async () => {
      await poolsPage.validatePoolRowCount(3);
      await poolsPage.validatePoolRowDetails(1, poolName1, testConfig.pool.url);
      await poolsPage.validatePoolRowDetails(2, poolName2, testConfig.pool.url);
    });
  });
});
