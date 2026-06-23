import { testConfig } from "../config/test.config";
import { expect, test } from "../fixtures/pageFixtures";
import { generateRandomText } from "../helpers/testDataHelper";
import {
  type CurtailmentCleanupTarget,
  getStartCurtailmentRequestBody,
  getStartCurtailmentResponseBody,
} from "../pages/energy";

const CURTAILMENT_PREFIX = "curtailment_e2e";

test.describe("Proto Fleet - Curtailment", () => {
  let startedCurtailment: CurtailmentCleanupTarget | undefined;

  test.beforeEach(async ({ page }) => {
    startedCurtailment = undefined;
    await page.goto("/");
  });

  test.afterEach("CLEANUP: Stop curtailments created during tests", async ({ energyPage }) => {
    if (!startedCurtailment) {
      return;
    }

    await energyPage.cleanupStartedCurtailment(startedCurtailment);
  });

  if (testConfig.target !== "real") {
    test("Start and stop a whole-fleet curtailment", async ({ commonSteps, energyPage, page }) => {
      test.setTimeout(testConfig.testTimeout * 2);

      const curtailmentReason = generateRandomText(CURTAILMENT_PREFIX);
      const targetKw = "1";
      const restoreBatchIntervalSec = "1";

      await test.step("Log in as an admin with curtailment permissions", async () => {
        await commonSteps.loginAsAdmin();
      });

      await test.step("Navigate to Energy", async () => {
        await energyPage.navigateToEnergyPage();
        await energyPage.validateEnergyPageOpened();
      });

      let startRequest!: Awaited<ReturnType<typeof page.waitForRequest>>;
      let startResponse!: Awaited<ReturnType<typeof page.waitForResponse>>;

      await test.step("Start a whole-fleet curtailment", async () => {
        await energyPage.openCurtailmentPlanner();
        await energyPage.fillCurtailmentPlan({
          reason: curtailmentReason,
          targetKw,
          restoreBatchIntervalSec,
        });
        await energyPage.waitForPreview(targetKw);
        startedCurtailment = { reason: curtailmentReason };
        [startRequest, startResponse] = await Promise.all([
          page.waitForRequest(/StartCurtailment/),
          page.waitForResponse(/StartCurtailment/),
          energyPage.startCurtailment(),
        ]);
      });

      await test.step("Validate the StartCurtailment request", async () => {
        const requestBody = getStartCurtailmentRequestBody(startRequest);
        const responseBody = await getStartCurtailmentResponseBody(startResponse);

        expect(startRequest.method()).toBe("POST");
        expect(requestBody.reason).toBe(curtailmentReason);
        expect(requestBody.mode).toBe("CURTAILMENT_MODE_FIXED_KW");
        expect(requestBody.fixedKw?.targetKw).toBe(Number(targetKw));
        expect(requestBody.wholeOrg).toEqual({});
        expect(requestBody.includeMaintenance).toBe(true);
        expect(requestBody.forceIncludeMaintenance).toBe(true);
        expect(requestBody.restoreBatchIntervalSec).toBe(Number(restoreBatchIntervalSec));
        expect(startResponse.status()).toBe(200);
        expect(responseBody.event?.eventUuid).toEqual(expect.any(String));
        expect(responseBody.event?.reason).toBe(curtailmentReason);
        startedCurtailment = {
          reason: curtailmentReason,
          eventUuid: responseBody.event?.eventUuid as string,
        };
      });

      await test.step("Validate active curtailment UI and history", async () => {
        await energyPage.validateActiveCurtailment(curtailmentReason);
        await energyPage.validateCurtailmentHistoryRow(curtailmentReason);
      });

      await test.step("Stop the curtailment", async () => {
        expect(startedCurtailment?.eventUuid).toEqual(expect.any(String));
        const curtailmentToStop = startedCurtailment as CurtailmentCleanupTarget;
        const expectedEventUuid = curtailmentToStop.eventUuid;
        const stopRequestPromise = page.waitForRequest(/StopCurtailment/);
        const stopResponsePromise = page.waitForResponse(/StopCurtailment/);

        await energyPage.stopCurtailment(curtailmentToStop);

        const stopRequest = await stopRequestPromise;
        const stopResponse = await stopResponsePromise;
        const stopBody = stopRequest.postDataJSON() as { eventUuid?: string; force?: boolean };

        expect(stopRequest.method()).toBe("POST");
        expect(stopBody.eventUuid).toBe(expectedEventUuid);
        expect(stopBody.force ?? false).toBe(false);
        expect(stopResponse.status()).toBe(200);

        await energyPage.waitForCurtailmentToRestore(curtailmentToStop);
        startedCurtailment = undefined;
      });
    });
  }
});
