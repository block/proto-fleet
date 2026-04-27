import { expect } from "@playwright/test";
import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";
import { CommonSteps } from "../helpers/commonSteps";
import { AuthPage } from "../pages/auth";
import { MinersPage } from "../pages/miners";

async function ensureVisibleMinersAwake(minersPage: MinersPage) {
  const hasSleepingMiners = await minersPage.hasAnyMinerWithStatus("Sleeping");
  const hasWakingMiners = await minersPage.hasAnyMinerWithStatus("Waking");

  if (!hasSleepingMiners && !hasWakingMiners) {
    return;
  }

  await minersPage.clickSelectAllCheckbox();
  await minersPage.clickActionsMenuButton();
  await minersPage.clickWakeUpButton();
  await minersPage.clickWakeUpConfirm();
  await minersPage.validateNoMinerWithStatus("Sleeping");
  await minersPage.validateNoMinerWithStatus("Waking");
}

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
      await ensureVisibleMinersAwake(minersPage);
    } finally {
      await context.close();
    }
  });

  test("SLEEP - WAKE a miner", async ({ minersPage, commonSteps }) => {
    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    await test.step("Filter Proto rig miners", async () => {
      await minersPage.filterRigMiners();
      await ensureVisibleMinersAwake(minersPage);
    });

    let minerIp: string;

    await test.step("Select first miner and shut it down", async () => {
      minerIp = await minersPage.getMinerIpAddressByStatus("Hashing");
      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.clickShutdownButton();
      await minersPage.clickShutdownConfirm();
    });

    await test.step("Validate update process", async () => {
      await minersPage.validateTextInToastGroup("Putting miners to sleep");
      await minersPage.validateTextInToastGroup("Put 1 out of 1 miners to sleep");
    });

    await test.step("Validate miner is sleeping", async () => {
      await minersPage.validateMinerStatusSettled(minerIp, "Sleeping");
    });

    await test.step("Select all miners and wake them up", async () => {
      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.clickWakeUpButton();
      await minersPage.clickWakeUpConfirm();
    });

    await test.step("Validate update process", async () => {
      await minersPage.validateTextInToastGroup("Waking up miners");
      await minersPage.validateTextInToastGroup(`Woke up 1 out of 1 miners`);
    });

    await test.step("Validate none of the miners are sleeping", async () => {
      await minersPage.validateMinerStatusSettled(minerIp, "Hashing");
      await minersPage.validateNoMinerWithStatus("Sleeping");
      await minersPage.validateNoMinerWithStatus("Waking");
    });
  });

  test("SLEEP - WAKE all rig miners, without page refresh", async ({ minersPage, commonSteps }) => {
    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    await test.step("Filter Proto rig miners", async () => {
      await minersPage.filterRigMiners();
      await ensureVisibleMinersAwake(minersPage);
    });

    let minerCount: number;

    await test.step("Select all miners and put them to sleep", async () => {
      await minersPage.clickSelectAllCheckbox();
      minerCount = await minersPage.getMinersCount();
      await minersPage.clickActionsMenuButton();
      await minersPage.clickShutdownButton();
      await minersPage.clickShutdownConfirm();
    });

    await test.step("Validate sleep process", async () => {
      await minersPage.validateTextInToastGroup("Putting miners to sleep");
      await minersPage.validateTextInToastGroup(`Put ${minerCount} out of ${minerCount} miners to sleep`);
    });

    await test.step("Validate all miners are sleeping", async () => {
      await minersPage.validateAllMinersStatusSettled("Sleeping");
    });

    await test.step("Select all miners and wake them up", async () => {
      await minersPage.clickSelectAllCheckbox();
      await minersPage.clickActionsMenuButton();
      await minersPage.clickWakeUpButton();
      await minersPage.clickWakeUpConfirm();
    });

    await test.step("Validate wake up process", async () => {
      await minersPage.validateTextInToastGroup("Waking up miners");
      await minersPage.validateTextInToastGroup(`Woke up ${minerCount} out of ${minerCount} miners`);
    });

    await test.step("Validate all miners are awake", async () => {
      await minersPage.validateAllMinersStatusSettled("Hashing");
    });
  });

  test("SLEEP - WAKE all non-rig miners, without page refresh", async ({ minersPage, commonSteps }) => {
    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    let initialMinerStatuses: Array<{ ipAddress: string; status: string }>;

    await test.step("Filter all miners except Proto Rig", async () => {
      await minersPage.filterAllMinersExceptRig();
      await ensureVisibleMinersAwake(minersPage);
      initialMinerStatuses = await minersPage.getVisibleMinerStatuses();
    });

    let minerCount: number;

    await test.step("Select all non-rig miners and put them to sleep", async () => {
      minerCount = initialMinerStatuses.length;
      expect(minerCount).toBeGreaterThan(0);

      await minersPage.clickSelectAllCheckbox();
      await minersPage.clickActionsMenuButton();
      await minersPage.clickShutdownButton();
      await minersPage.clickShutdownConfirm();
    });

    await test.step("Validate sleep process", async () => {
      await minersPage.validateTextInToastGroup("Putting miners to sleep");
      await minersPage.validateTextInToastGroup(`Put ${minerCount} out of ${minerCount} miners to sleep`);
    });

    await test.step("Validate all non-rig miners are sleeping", async () => {
      await minersPage.validateAllMinersStatusSettled("Sleeping");
    });

    await test.step("Select all non-rig miners and wake them up", async () => {
      await minersPage.clickSelectAllCheckbox();
      await minersPage.clickActionsMenuButton();
      await minersPage.clickWakeUpButton();
      await minersPage.clickWakeUpConfirm();
    });

    await test.step("Validate wake up process", async () => {
      await minersPage.validateTextInToastGroup("Waking up miners");
      await minersPage.validateTextInToastGroup(`Woke up ${minerCount} out of ${minerCount} miners`);
    });

    await test.step("Validate all non-rig miners returned to their initial statuses", async () => {
      for (const miner of initialMinerStatuses) {
        await minersPage.validateMinerStatusSettled(miner.ipAddress, miner.status);
      }
    });
  });
});
