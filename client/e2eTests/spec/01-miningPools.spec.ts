/* eslint-disable playwright/expect-expect */
import { generateRandomText } from "e2eTests/helpers/testDataHelper";
import { test } from "../fixtures/pageFixtures";

test.describe("Mining Pools @setup", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  const invalidPoolUrl = "stratum+tcp://eu1.examplepool.com:3333";
  const validPoolUrl = "stratum+tcp://mine.ocean.xyz:3334";

  test("Configure mining pool", async ({ settingsPage, settingsPoolsPage, newPoolModal, commonSteps }) => {
    const settingsPoolName = generateRandomText("PoolName");
    const poolUsername = generateRandomText("PoolUsername");
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

  test("Add default mining pool to all miners", async ({ minersPage, editPoolPage, newPoolModal, commonSteps }) => {
    const poolName = generateRandomText("PoolName");
    const poolUsername = generateRandomText("PoolUsername");
    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

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
      await newPoolModal.clickSaveNewPool();
      await editPoolPage.clickAssignToXMiners(amountOfMiners);
    });

    await test.step("Validate the pool has been assigned", async () => {
      await minersPage.validateNoMinerWithStatus("Needs mining pool");
    });
  });

  test("Add to first miner backup pool created from settings", async ({
    settingsPage,
    settingsPoolsPage,
    newPoolModal,
    minersPage,
    editPoolPage,
    commonSteps,
  }) => {
    const settingsPoolName = generateRandomText("PoolName");
    const poolUsername = generateRandomText("PoolUsername");
    await commonSteps.loginAsAdmin();

    await test.step("Navigate to mining pools settings", async () => {
      await settingsPage.navigateToMiningPoolsSettings();
      await settingsPoolsPage.validateMiningPoolsPageOpened();
    });

    await test.step("Add a pool", async () => {
      await settingsPoolsPage.clickAddPool();
      await newPoolModal.inputPoolName(settingsPoolName);
      await newPoolModal.inputPoolUrl(validPoolUrl);
      await newPoolModal.inputPoolUsername(poolUsername);
      await newPoolModal.clickSaveNewPool();
      await settingsPoolsPage.validatePoolEntryByUniqueName(settingsPoolName, validPoolUrl, poolUsername);
    });

    await commonSteps.goToMinersPage();

    let minerIp: string;
    let minerStatus: string;

    await test.step("Open pool editor for first miner", async () => {
      minerIp = await minersPage.getMinerIpAddressByIndex(0);
      minerStatus = await minersPage.getMinerStatus(minerIp);
      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.clickEditMiningPoolButton();
    });

    await test.step("Select backup mining pool", async () => {
      await editPoolPage.clickAddBackupPoolOne();
      await editPoolPage.clickPoolRowByName(settingsPoolName);
      await editPoolPage.clickSavePoolChoice();
      await editPoolPage.clickAssignToXMiners(1);
    });

    await test.step("Validate miner's status did not change", async () => {
      await minersPage.validateMinerStatus(minerIp, minerStatus);
    });
  });
});
