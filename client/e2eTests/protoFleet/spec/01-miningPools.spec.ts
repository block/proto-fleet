/* eslint-disable playwright/expect-expect */
import { DEFAULT_INTERVAL, testConfig } from "../config/test.config";
import { expect, test } from "../fixtures/pageFixtures";
import { CommonSteps } from "../helpers/commonSteps";
import { generateRandomText } from "../helpers/testDataHelper";
import { AuthPage } from "../pages/auth";
import { LoginModalComponent } from "../pages/components/loginModal";
import { EditPoolPage } from "../pages/editPool";
import { MinersPage } from "../pages/miners";
import { NewPoolModalPage } from "../pages/newPoolModal";
import { SettingsPage } from "../pages/settings";
import { SettingsPoolsPage } from "../pages/settingsPools";

function generatePoolUsername(): string {
  return generateRandomText("PoolUsername");
}

test.describe("Mining Pools", () => {
  test.beforeEach(async ({ page, settingsPage, settingsPoolsPage, commonSteps }) => {
    await page.goto("/");

    // Clear all existing pools to ensure consistent test state
    await commonSteps.loginAsAdmin();
    await settingsPage.navigateToMiningPoolsSettings();
    await settingsPoolsPage.validateMiningPoolsPageOpened();
    await settingsPoolsPage.deleteAllPools();
    await page.goto("/");
  });

  test.afterAll("CLEANUP: Add default pool to miners", async ({ browser }, testInfo) => {
    const isMobile = testInfo.project.use?.isMobile ?? false;
    const context = await browser.newContext({ baseURL: testConfig.baseUrl });
    try {
      const page = await context.newPage();
      await page.goto("/");

      const authPage = new AuthPage(page, isMobile);
      const minersPage = new MinersPage(page, isMobile);
      const editPoolPage = new EditPoolPage(page, isMobile);
      const newPoolModal = new NewPoolModalPage(page, isMobile);
      const loginModal = new LoginModalComponent(page, isMobile);
      const settingsPage = new SettingsPage(page, isMobile);
      const settingsPoolsPage = new SettingsPoolsPage(page, isMobile);
      const commonSteps = new CommonSteps(authPage, minersPage);

      await commonSteps.loginAsAdmin();

      await commonSteps.goToMinersPage();

      const amountOfMiners = await minersPage.getMinersCount();
      if (amountOfMiners > 0) {
        await minersPage.clickSelectAllCheckbox();
        await minersPage.clickActionsMenuButton();
        await minersPage.clickEditMiningPoolButton();
        await loginModal.loginAsAdmin();

        await editPoolPage.clickAddPoolButton();
        await editPoolPage.clickAddNewPool();
        await newPoolModal.inputPoolName("PoolNameDefault");
        await newPoolModal.inputPoolUrl(validPoolUrl);

        await newPoolModal.inputPoolUsername(generateRandomText("Afterhook"));
        // await newPoolModal.inputPoolUsername(validUsername); // use when DASH-1407 is fixed
        await newPoolModal.clickSaveNewPool();
        await editPoolPage.clickAssignToXMiners(amountOfMiners);
        await editPoolPage.validateTextInToastGroup("Assigned pools");
      }

      await settingsPage.navigateToMiningPoolsSettings();
      await settingsPoolsPage.validateMiningPoolsPageOpened();

      const poolRows = page.getByTestId("pool-row");
      const poolCount = await poolRows.count();

      for (let i = poolCount - 1; i >= 0; i--) {
        const row = poolRows.nth(i);
        const poolNameElement = row.getByTestId("pool-name");
        const poolName = await poolNameElement.textContent();

        if (poolName && poolName.startsWith("PoolName")) {
          await row.getByRole("button", { name: "Options menu", exact: true }).click();
          await settingsPoolsPage.clickButton("Delete pool");
        }
      }
    } finally {
      await context.close();
    }
  });

  const invalidPoolUrl = "stratum+tcp://eu1.examplepool.com:3333";
  const validPoolUrl = "stratum+tcp://mine.ocean.xyz:3334";
  // When DASH-1407 is fixed, use a real wallet, so that real miners always have it configured
  // Also, removed the actual username for security reasons. Need to get from GH secrets
  // const validUsername = "aaaaaaa";

  test("Configure mining pool", async ({ settingsPage, settingsPoolsPage, newPoolModal }) => {
    const settingsPoolName = generateRandomText("PoolName");
    const poolUsername = generatePoolUsername();

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

    await test.step("Change URL to a valid one", async () => {
      await newPoolModal.inputPoolUrl(validPoolUrl);
    });

    await test.step("Save and validate pool URL", async () => {
      await newPoolModal.clickSaveNewPool();
      await settingsPoolsPage.validatePoolEntryByUniqueName(settingsPoolName, validPoolUrl, poolUsername);
    });
  });

  test("Add default mining pool to all miners @setup", async ({
    minersPage,
    editPoolPage,
    newPoolModal,
    loginModal,
    commonSteps,
  }) => {
    const poolName = generateRandomText("PoolName");
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
      await newPoolModal.inputPoolUsername(generateRandomText("allMinerDefault"));
      // await newPoolModal.inputPoolUsername(validUsername); // use when DASH-1407 is fixed
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
    const newPoolName1 = generateRandomText("PoolName1");
    const newPoolName2 = generateRandomText("PoolName2");
    const newPoolUsername1 = generatePoolUsername();
    const newPoolUsername2 = generatePoolUsername();

    await test.step("Navigate to mining pools settings", async () => {
      await settingsPage.navigateToMiningPoolsSettings();
      await settingsPoolsPage.validateMiningPoolsPageOpened();
    });

    await test.step("Add a pool", async () => {
      await settingsPoolsPage.clickAddPool();
      await newPoolModal.inputPoolName(newPoolName1);
      await newPoolModal.inputPoolUrl(validPoolUrl);
      await newPoolModal.inputPoolUsername(newPoolUsername1);
      await newPoolModal.clickSaveNewPool();
      await settingsPoolsPage.validatePoolEntryByUniqueName(newPoolName1, validPoolUrl, newPoolUsername1);
      await settingsPoolsPage.validateTextInToast("Pool added");
    });

    await commonSteps.goToMinersPage();

    let minerIp: string;
    let minerStatus: string;

    await test.step("Open pool editor for first miner", async () => {
      minerIp = await minersPage.getMinerIpAddressByIndex(0);
      minerStatus = await minersPage.getMinerStatus(minerIp);
      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.clickEditMiningPoolButton();
      await loginModal.loginAsAdmin();
    });

    await test.step("Remove all existing pools from miner", async () => {
      await editPoolPage.removeAllPools();
    });

    await test.step("Add first pool to the miner", async () => {
      await editPoolPage.clickAddPoolButton();
      await editPoolPage.validateModalIsOpen();
      await editPoolPage.clickPoolRowByName(newPoolName1);
      await editPoolPage.clickSavePoolChoice();
      await editPoolPage.validateModalIsClosed();
      await editPoolPage.validatePoolCount(1);
    });

    await test.step("Add another pool to the miner", async () => {
      await editPoolPage.clickAddAnotherPoolButton();
      await editPoolPage.clickAddNewPool();
      await editPoolPage.validateModalIsOpen();
      await newPoolModal.inputPoolName(newPoolName2);
      await newPoolModal.inputPoolUrl(validPoolUrl);
      await newPoolModal.inputPoolUsername(newPoolUsername2);
      await newPoolModal.clickSaveNewPool();
      await editPoolPage.validateModalIsClosed();
    });

    await test.step("Validate pool order", async () => {
      await editPoolPage.validatePoolCount(2);
      await editPoolPage.validatePoolByIndex(0, newPoolName1, validPoolUrl);
      await editPoolPage.validatePoolByIndex(1, newPoolName2, validPoolUrl);
    });

    await test.step("Reorder mining pools", async () => {
      await editPoolPage.reorderPoolByDragging(1, 0);
    });

    await test.step("Validate pool order after reorder", async () => {
      await editPoolPage.validatePoolCount(2);
      await editPoolPage.validatePoolByIndex(0, newPoolName2, validPoolUrl);
      await editPoolPage.validatePoolByIndex(1, newPoolName1, validPoolUrl);
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
      expect(await editPoolPage.getPoolUrlByIndex(1)).toBe(validPoolUrl);
    });
  });
});
