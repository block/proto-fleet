/* eslint-disable playwright/expect-expect */
import { generateRandomText } from "e2eTests/helpers/testDataHelper";
import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";

test.describe("Mining Pools @setup", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });
  const invalidPoolUrl = "stratum+tcp://eu1.examplepool.com:3333";
  const validPoolUrl = "stratum+tcp://stratum.slushpool.com:3333";

  test("Configure mining pool", async ({ authPage, settingsPage, settingsPoolsPage, newPoolModal }) => {
    const poolName = generateRandomText("PoolName");
    const poolUsername = "test";
    await test.step("Login as admin", async () => {
      await authPage.inputUsername(testConfig.users.admin.username);
      await authPage.inputPassword(testConfig.users.admin.password);
      await authPage.clickLogin();
      await authPage.validateLoggedIn();
    });

    await test.step("Navigate to mining pools settings", async () => {
      await authPage.navigateToSettingsPage();
      await settingsPage.navigateToMiningPoolsSettings();
      await settingsPoolsPage.validateMiningPoolsPageOpened();
    });

    await test.step("Configure mining pool with invalid URL", async () => {
      await settingsPoolsPage.clickAddPool();
      await newPoolModal.validatePoolModalOpened();
      await newPoolModal.inputPoolName(poolName);
      await newPoolModal.inputPoolUrl(invalidPoolUrl);
      await newPoolModal.inputPoolUsername(poolUsername);
    });

    await test.step("Test connection - expect failure", async () => {
      await newPoolModal.clickTestConnection();
      await newPoolModal.validateConnectionFailed();
    });

    await test.step("Change URL to a valid one", async () => {
      await newPoolModal.inputPoolUrl(validPoolUrl);
    });

    await test.step("Test connection - expect success", async () => {
      await newPoolModal.clickTestConnection();
      await newPoolModal.validateConnectionSuccessful();
    });

    await test.step("Save and validate pool URL", async () => {
      await newPoolModal.clickSavePool();
      await settingsPoolsPage.validatePoolEntryByUniqueName(poolName, validPoolUrl, poolUsername);
    });
  });

  test("Add default mining pool to all miners", async ({ authPage, minersPage, editPoolPage, newPoolModal }) => {
    const poolName = generateRandomText("PoolName");
    const poolUsername = "pool";
    await test.step("Login as admin", async () => {
      await authPage.inputUsername(testConfig.users.admin.username);
      await authPage.inputPassword(testConfig.users.admin.password);
      await authPage.clickLogin();
      await authPage.validateLoggedIn();
    });

    await test.step("Navigate to miners page", async () => {
      await authPage.navigateToMinersPage();
      await minersPage.waitForMinersTitle();
      await minersPage.waitForMinersListToLoad();
    });

    let amountOfMiners: number;
    await test.step("Select all miners and open pool editor", async () => {
      amountOfMiners = await minersPage.getMinersCount();
      await minersPage.clickSelectAllCheckbox();
      await minersPage.clickActionsMenuButton();
      await minersPage.clickEditMiningPoolButton();
    });

    await test.step("Add default mining pool", async () => {
      await editPoolPage.clickAddDefaultMiningPool();
      await editPoolPage.clickAddNewPool();
      await newPoolModal.inputPoolName(poolName);
      await newPoolModal.inputPoolUrl(validPoolUrl);
      await newPoolModal.inputPoolUsername(poolUsername);
      await newPoolModal.clickTestConnection();
      await newPoolModal.validateConnectionSuccessful();
      await newPoolModal.clickSavePool();
      await editPoolPage.clickAssignToXMiners(amountOfMiners);
    });

    await test.step("Validate the pool has been assigned", async () => {
      await minersPage.validateNoMinerWithStatus("Needs mining pool");
    });
  });
});
