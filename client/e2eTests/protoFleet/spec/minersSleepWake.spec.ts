import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";
import { CommonSteps } from "../helpers/commonSteps";
import { AuthPage } from "../pages/auth";
import { MinersPage } from "../pages/miners";

test.describe("Miners SLEEP - WAKE actions", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test.afterEach("CLEANUP: wake up miners", async ({ browser }, testInfo) => {
    if (testConfig.target === "real") {
      return;
    }

    const isMobile = testInfo.project.use?.isMobile ?? false;
    const context = await browser.newContext({ baseURL: testConfig.baseUrl });
    try {
      const page = await context.newPage();
      await page.goto("/");

      const authPage = new AuthPage(page, isMobile);
      const minersPage = new MinersPage(page, isMobile);
      const commonSteps = new CommonSteps(authPage, minersPage);

      await commonSteps.loginAsAdmin();
      await commonSteps.goToMinersPage();
      await minersPage.filterRigMiners();
      const minerCount = await minersPage.getMinersCount();

      if (minerCount > 0) {
        await minersPage.clickSelectAllCheckbox();
        await minersPage.clickActionsMenuButton();
        await minersPage.clickWakeUpButton();
        await minersPage.clickWakeUpConfirm();
        await minersPage.validateNoMinerWithStatus("Sleeping");
        await minersPage.validateNoMinerWithStatus("Waking");
      }
    } finally {
      await context.close();
    }
  });

  test("SLEEP - WAKE a miner @smoke", async ({ minersPage, commonSteps, page }) => {
    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    await test.step("Filter Proto miners as a workaround", async () => {
      // Workaround: Antminer miners don't support SLEEP action
      await minersPage.filterRigMiners();
    });

    let minerIp: string;
    const shutdownRequestPromise = page.waitForRequest(/StopMining/);
    const shutdownResponsePromise = page.waitForResponse(/StopMining/);

    await test.step("Select first miner and shut it down", async () => {
      minerIp = await minersPage.getMinerIpAddressByIndex(0);
      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.clickShutdownButton();
      await minersPage.clickShutdownConfirm();
    });

    await test.step("Validate shutdown API request", async () => {
      const request = await shutdownRequestPromise;
      const response = await shutdownResponsePromise;
      const requestBody = request.postDataJSON();
      test.expect(request.method()).toBe("POST");
      test.expect(requestBody).toHaveProperty("deviceSelector");
      test.expect(response.status()).toBe(200);
    });

    await test.step("Validate miner is sleeping", async () => {
      await minersPage.validateMinerStatus(minerIp, "Sleeping");
    });

    const wakeUpRequestPromise = page.waitForRequest(/StartMining/);
    const wakeUpResponsePromise = page.waitForResponse(/StartMining/);

    await test.step("Select all miners and wake them up", async () => {
      await minersPage.clickSelectAllCheckbox();
      await minersPage.clickActionsMenuButton();
      await minersPage.clickWakeUpButton();
      await minersPage.clickWakeUpConfirm();
    });

    await test.step("Validate wake-up API request", async () => {
      const request = await wakeUpRequestPromise;
      const response = await wakeUpResponsePromise;
      const requestBody = request.postDataJSON();
      test.expect(request.method()).toBe("POST");
      test.expect(requestBody).toHaveProperty("deviceSelector");
      test.expect(response.status()).toBe(200);
    });

    await test.step("Validate none of the miners are sleeping", async () => {
      await minersPage.validateMinerStatus(minerIp, "Hashing");
      await minersPage.validateNoMinerWithStatus("Sleeping");
      await minersPage.validateNoMinerWithStatus("Waking");
    });
  });

  test("SLEEP - WAKE all rig miners, without page refresh", async ({ minersPage, commonSteps, page }) => {
    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    await test.step("Filter Proto miners as a workaround", async () => {
      // Workaround: Antminer miners don't support SLEEP action
      await minersPage.filterRigMiners();
    });

    const shutdownRequestPromise = page.waitForRequest(/StopMining/);
    const shutdownResponsePromise = page.waitForResponse(/StopMining/);

    await test.step("Select all miners and put them to sleep", async () => {
      await minersPage.clickSelectAllCheckbox();
      await minersPage.clickActionsMenuButton();
      await minersPage.clickShutdownButton();
      await minersPage.clickShutdownConfirm();
    });

    await test.step("Validate shutdown API request", async () => {
      const request = await shutdownRequestPromise;
      const response = await shutdownResponsePromise;
      const requestBody = request.postDataJSON();
      test.expect(request.method()).toBe("POST");
      test.expect(requestBody).toHaveProperty("deviceSelector");
      test.expect(response.status()).toBe(200);
    });

    await test.step("Validate all miners are sleeping", async () => {
      await minersPage.waitForAllStatusSpinnersToDisappear();
      await minersPage.validateAllMinersStatus("Sleeping");
    });

    const wakeUpRequestPromise = page.waitForRequest(/StartMining/);
    const wakeUpResponsePromise = page.waitForResponse(/StartMining/);

    await test.step("Select all miners and wake them up", async () => {
      await minersPage.clickSelectAllCheckbox();
      await minersPage.clickActionsMenuButton();
      await minersPage.clickWakeUpButton();
      await minersPage.clickWakeUpConfirm();
    });

    await test.step("Validate wake-up API request", async () => {
      const request = await wakeUpRequestPromise;
      const response = await wakeUpResponsePromise;
      const requestBody = request.postDataJSON();
      test.expect(request.method()).toBe("POST");
      test.expect(requestBody).toHaveProperty("deviceSelector");
      test.expect(response.status()).toBe(200);
    });

    await test.step("Validate all miners are awake", async () => {
      await minersPage.validateAllMinersStatus("Hashing");
    });
  });
});
