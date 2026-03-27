/* eslint-disable playwright/expect-expect */
import { DEFAULT_INTERVAL, testConfig } from "../config/test.config";
import { expect, test } from "../fixtures/pageFixtures";
import { generateRandomText } from "../helpers/testDataHelper";

function generatePoolUsername(): string {
  return generateRandomText("PoolUsername");
}

if (testConfig.target !== "real") {
  test.describe("Mining Pools", () => {
    test.beforeEach(async ({ page }) => {
      await page.goto("/");
    });

    const invalidPoolUrl = "stratum+tcp://eu1.examplepool.com:3333";
    const validPoolUrl = "stratum+tcp://mine.ocean.xyz:3334";

    test("Configure mining pool @smoke", async ({ settingsPage, settingsPoolsPage, newPoolModal, commonSteps }) => {
      const settingsPoolName = generateRandomText("PoolName");
      const poolUsername = generatePoolUsername();
      await commonSteps.loginAsAdmin();

      await test.step("Navigate to mining pools settings", async () => {
        await settingsPage.navigateToMiningPoolsSettings();
        await settingsPoolsPage.validateMiningPoolsPageOpened();
      });

      await test.step("Start adding a pool", async () => {
        await settingsPoolsPage.clickAddPool();
        await newPoolModal.validatePoolModalOpened();
      });

      await test.step("Validate empty pool url message", async () => {
        await newPoolModal.clickTestConnection();
        await newPoolModal.validateEmptyPoolUrlError();
      });

      await test.step("Configure mining pool with invalid URL", async () => {
        await newPoolModal.inputPoolName(settingsPoolName);
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
        await newPoolModal.clickSaveNewPool();
        await settingsPoolsPage.validatePoolEntryByUniqueName(settingsPoolName, validPoolUrl, poolUsername);
      });
    });

    test("Add default mining pool to all miners @setup @smoke", async ({
      minersPage,
      editPoolPage,
      newPoolModal,
      loginModal,
      commonSteps,
    }) => {
      const poolName = generateRandomText("PoolName");
      const poolUsername = generatePoolUsername();
      await commonSteps.loginAsAdmin();
      await commonSteps.goToMinersPage();

      let amountOfMiners: number;

      await test.step("Select all miners and open pool editor", async () => {
        amountOfMiners = await minersPage.getMinersCount();
        await minersPage.clickSelectAllCheckbox();
        await minersPage.clickActionsMenuButton();
        await minersPage.clickEditMiningPoolButton();
        await loginModal.loginAsAdmin();
      });

      await test.step("Add default mining pool", async () => {
        await editPoolPage.clickAddPoolButton();
        await editPoolPage.clickAddNewPool();
        await editPoolPage.validateModalIsOpen();
        await newPoolModal.inputPoolName(poolName);
        await newPoolModal.inputPoolUrl(validPoolUrl);
        await newPoolModal.inputPoolUsername(poolUsername);
        await newPoolModal.clickTestConnection();
        await newPoolModal.validateConnectionSuccessful();
        await newPoolModal.clickSaveNewPool();
        await editPoolPage.validateModalIsClosed();
        await editPoolPage.validatePoolByIndex(0, poolName, validPoolUrl);
        await editPoolPage.clickAssignToXMiners(amountOfMiners);
        await editPoolPage.validateTextInToastGroup("Assigned pools");
      });

      await test.step("Validate the pool has been assigned", async () => {
        await minersPage.validateNoMinerWithIssue("Pool required");
      });
    });

    test("Add pool created from settings and reorder", async ({
      settingsPage,
      settingsPoolsPage,
      newPoolModal,
      minersPage,
      editPoolPage,
      commonSteps,
      loginModal,
    }) => {
      const newPoolName = generateRandomText("PoolName");
      const newPoolUsername = generatePoolUsername();
      await commonSteps.loginAsAdmin();

      await test.step("Navigate to mining pools settings", async () => {
        await settingsPage.navigateToMiningPoolsSettings();
        await settingsPoolsPage.validateMiningPoolsPageOpened();
      });

      await test.step("Add a pool", async () => {
        await settingsPoolsPage.clickAddPool();
        await newPoolModal.inputPoolName(newPoolName);
        await newPoolModal.inputPoolUrl(validPoolUrl);
        await newPoolModal.inputPoolUsername(newPoolUsername);
        await newPoolModal.clickSaveNewPool();
        await settingsPoolsPage.validatePoolEntryByUniqueName(newPoolName, validPoolUrl, newPoolUsername);
        await settingsPoolsPage.validateTextInToast("Pool added");
      });

      await commonSteps.goToMinersPage();

      let minerIp: string;
      let minerStatus: string;
      let existingPoolName: string;
      let existingPoolUrl: string;

      await test.step("Open pool editor for first miner", async () => {
        minerIp = await minersPage.getMinerIpAddressByIndex(0);
        minerStatus = await minersPage.getMinerStatus(minerIp);
        await minersPage.clickMinerThreeDotsButton(minerIp);
        await minersPage.clickEditMiningPoolButton();
        await loginModal.loginAsAdmin();
      });

      await test.step("Get existing pool details", async () => {
        await editPoolPage.validatePoolCount(1);
        existingPoolName = await editPoolPage.getPoolNameByIndex(0);
        existingPoolUrl = await editPoolPage.getPoolUrlByIndex(0);
      });

      await test.step("Add another pool to the miner", async () => {
        await editPoolPage.clickAddAnotherPoolButton();
        await editPoolPage.validateModalIsOpen();
        await editPoolPage.clickPoolRowByName(newPoolName);
        await editPoolPage.clickSavePoolChoice();
        await editPoolPage.validateModalIsClosed();
      });

      await test.step("Validate pool order", async () => {
        await editPoolPage.validatePoolCount(2);
        await editPoolPage.validatePoolByIndex(0, existingPoolName, existingPoolUrl);
        await editPoolPage.validatePoolByIndex(1, newPoolName, validPoolUrl);
      });

      await test.step("Reorder mining pools", async () => {
        await editPoolPage.reorderPoolByDragging(1, 0);
      });

      await test.step("Validate pool order after reorder", async () => {
        await editPoolPage.validatePoolCount(2);
        await editPoolPage.validatePoolByIndex(0, newPoolName, validPoolUrl);
        await editPoolPage.validatePoolByIndex(1, existingPoolName, existingPoolUrl);
      });

      await test.step("Save pool changes", async () => {
        await new Promise((resolve) => setTimeout(resolve, DEFAULT_INTERVAL));
        await editPoolPage.clickAssignToXMiners(1);
        await editPoolPage.validateTextInToastGroup("Assigned pools");
      });

      await test.step("Validate miner's status did not change", async () => {
        await minersPage.validateMinerStatus(minerIp, minerStatus);
      });

      await test.step("Reopen miner and validate the pools have been saved successfully", async () => {
        await minersPage.clickMinerThreeDotsButton(minerIp);
        await minersPage.clickEditMiningPoolButton();
        await loginModal.loginAsAdmin();
        await editPoolPage.validatePoolCount(2);
        expect(await editPoolPage.getPoolUrlByIndex(0)).toBe(validPoolUrl);
        expect(await editPoolPage.getPoolUrlByIndex(1)).toBe(existingPoolUrl);
      });
    });
  });
}
