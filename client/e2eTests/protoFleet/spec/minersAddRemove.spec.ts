import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";
import { CommonSteps } from "../helpers/commonSteps";
import { AddMinersPage } from "../pages/addMiners";
import { AuthPage } from "../pages/auth";
import { HomePage } from "../pages/home";
import { MinersPage } from "../pages/miners";

test.describe("Miners UNPAIR - ADD actions", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test.afterEach("CLEANUP: Add miners, re-authenticate", async ({ browser }, testInfo) => {
    if (testConfig.target === "real") {
      return;
    }

    const isMobile = testInfo.project.use?.isMobile ?? false;
    const context = await browser.newContext({ baseURL: testConfig.baseUrl });
    try {
      const page = await context.newPage();
      await page.goto("/");

      const homePage = new HomePage(page, isMobile);
      const authPage = new AuthPage(page, isMobile);
      const minersPage = new MinersPage(page, isMobile);
      const addMinersPage = new AddMinersPage(page, isMobile);
      const commonSteps = new CommonSteps(authPage, minersPage);

      await commonSteps.loginAsAdmin();

      // Step 1: Add miners from network if any are available
      await minersPage.navigateToMinersPage();

      const addMinersButtonClicked = await minersPage.tryAction(() => minersPage.clickAddMinersButton());
      if (!addMinersButtonClicked) {
        await authPage.clickGetStarted();
      }
      await addMinersPage.clickFindMinersInNetwork();
      await addMinersPage.waitForFoundMinersList();
      const foundMinerCount = await addMinersPage.getFoundMinersCount();

      if (foundMinerCount === 0) {
        await addMinersPage.clickHeaderIconButton();
      } else {
        await addMinersPage.clickContinueWithSelectedMiners();
      }
      await minersPage.waitForMinersListToLoad();

      // Step 2: Re-authenticate miners if needed (existing logic)
      const authenticateMinersButtonClicked = await homePage.tryAction(() => homePage.clickAuthenticateMinersButton());
      if (authenticateMinersButtonClicked) {
        await homePage.validateAuthenticateMinersModalTitle();
        await homePage.clickShowMinersButton();
        const miners = await homePage.getListOfMinersToAuthenticate();

        if (miners.some((miner) => miner.includes("S17 XP"))) {
          await homePage.inputMinerAuthUsername("root17");
          await homePage.inputMinerAuthPassword("root17");
          await homePage.clickAuthenticateMinersConfirmButton();
        }
        if (miners.some((miner) => miner.includes("S19 XP"))) {
          await homePage.inputMinerAuthUsername("root19");
          await homePage.inputMinerAuthPassword("root19");
          await homePage.clickAuthenticateMinersConfirmButton();
        }
        if (miners.some((miner) => miner.includes("S21 XP"))) {
          await homePage.inputMinerAuthUsername("root21");
          await homePage.inputMinerAuthPassword("root21");
          await homePage.clickAuthenticateMinersConfirmButton();
        }
        await homePage.validateModalClosed();
      }
    } finally {
      await context.close();
    }
  });

  test("UNPAIR - ADD a single miner", async ({ minersPage, commonSteps, addMinersPage }) => {
    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    let minerCount: number;
    let minerIp: string;

    await test.step("Select a miner and unpair it", async () => {
      minerCount = await minersPage.getMinersCount();
      minerIp = await minersPage.getAuthenticatedMinerIpAddressByIndex(0);
      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.clickUnpairButton();
      await minersPage.clickUnpairConfirm();
    });

    await test.step("Validate miner was unpaired", async () => {
      await minersPage.validateMinerNotPresent(minerIp);
      await minersPage.validateAmountOfMiners(minerCount - 1);
    });

    await test.step("Add a single miner", async () => {
      await minersPage.clickAddMinersButton();
      await addMinersPage.clickFindMinersInNetwork();
      await addMinersPage.clickContinueWithXMiners(1);
    });

    await test.step("Validate miner was added", async () => {
      await minersPage.waitForMinersTitle();
      await minersPage.waitForMinersListToLoad();
      await minersPage.validateMinerInList(minerIp);
      await minersPage.validateAmountOfMiners(minerCount);
    });
  });

  test("UNPAIR - ADD multiple miners", async ({ minersPage, commonSteps, addMinersPage }) => {
    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    let minerCount: number;
    let minerIp1: string;
    let minerIp2: string;
    let minerIp3: string;

    await test.step("Select multiple miners and unpair them", async () => {
      minerCount = await minersPage.getMinersCount();
      minerIp1 = await minersPage.getAuthenticatedMinerIpAddressByIndex(0);
      minerIp2 = await minersPage.getAuthenticatedMinerIpAddressByIndex(1);
      minerIp3 = await minersPage.getAuthenticatedMinerIpAddressByIndex(2);
      await minersPage.clickMinerCheckbox(minerIp1);
      await minersPage.validateActionBarMinerCount(1);
      await minersPage.clickMinerCheckbox(minerIp2);
      await minersPage.validateActionBarMinerCount(2);
      await minersPage.clickMinerCheckbox(minerIp3);
      await minersPage.validateActionBarMinerCount(3);
      await minersPage.clickActionsMenuButton();
      await minersPage.clickUnpairButton();
      await minersPage.clickUnpairConfirm();
    });

    await test.step("Validate miners were unpaired", async () => {
      await minersPage.validateMinerNotPresent(minerIp1);
      await minersPage.validateMinerNotPresent(minerIp2);
      await minersPage.validateMinerNotPresent(minerIp3);
      await minersPage.validateAmountOfMiners(minerCount - 3);
    });

    await test.step("Add multiple miners", async () => {
      await minersPage.clickAddMinersButton();
      await addMinersPage.clickFindMinersInNetwork();
      await addMinersPage.clickChooseMiners();
      await addMinersPage.clickSelectNone();
      await addMinersPage.clickMinerCheckbox(minerIp1);
      await addMinersPage.clickMinerCheckbox(minerIp2);
      await addMinersPage.clickMinerCheckbox(minerIp3);
      await addMinersPage.clickDone();
      await addMinersPage.clickContinueWithXMiners(3);
    });

    await test.step("Validate miners were added", async () => {
      await minersPage.waitForMinersTitle();
      await minersPage.waitForMinersListToLoad();
      await minersPage.validateMinerInList(minerIp1);
      await minersPage.validateMinerInList(minerIp2);
      await minersPage.validateMinerInList(minerIp3);
      await minersPage.validateAmountOfMiners(minerCount);
    });
  });

  test("UNPAIR - ADD all miners", async ({ minersPage, commonSteps, addMinersPage }) => {
    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    let originalMinerCount: number;
    const allMinerIps: string[] = [];

    await test.step("Capture all miner IPs and select all miners", async () => {
      originalMinerCount = await minersPage.getMinersCount();

      for (let i = 0; i < originalMinerCount; i++) {
        const minerIp = await minersPage.getMinerIpAddressByIndex(i);
        allMinerIps.push(minerIp);
      }

      await minersPage.clickSelectAllCheckbox();
      await minersPage.validateActionBarMinerCount(originalMinerCount);
    });

    await test.step("Unpair all miners", async () => {
      await minersPage.clickActionsMenuButton();
      await minersPage.clickUnpairButton();
      await minersPage.clickUnpairConfirm();
    });

    await test.step("Validate all miners were unpaired", async () => {
      for (const minerIp of allMinerIps) {
        await minersPage.validateMinerNotPresent(minerIp);
      }
      await minersPage.validateAmountOfMiners(0);
    });

    await test.step("Validate null state - no miners added", async () => {
      await minersPage.validateTextIsVisible("You haven't paired any miners");
      await minersPage.validateTextIsVisible("Add miners to your fleet to get started.");
    });

    await test.step("Add all miners back using onboarding flow", async () => {
      await minersPage.clickGetStarted();
      await addMinersPage.clickFindMinersInNetwork();
      await addMinersPage.clickContinueWithSelectedMiners();
    });

    await test.step("Validate all miners were added back", async () => {
      await minersPage.waitForMinersTitle();
      await minersPage.waitForMinersListToLoad();
      await minersPage.validateMinersAdded(originalMinerCount);

      for (const minerIp of allMinerIps) {
        await minersPage.validateMinerInList(minerIp);
      }
    });
  });
});
