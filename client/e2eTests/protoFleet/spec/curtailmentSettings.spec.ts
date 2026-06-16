import { testConfig } from "../config/test.config";
import { expect, test } from "../fixtures/pageFixtures";
import { CommonSteps } from "../helpers/commonSteps";
import { generateRandomText } from "../helpers/testDataHelper";
import { AuthPage } from "../pages/auth";
import { MinersPage } from "../pages/miners";
import { SettingsCurtailmentPage } from "../pages/settingsCurtailment";

const RESPONSE_PROFILE_PREFIX = "curtailment_profile_e2e";
const SOURCE_PREFIX = "curtailment_source_e2e";

test.describe("Proto Fleet - Curtailment Settings", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test.afterEach("CLEANUP: Delete curtailment settings created during tests", async ({ browser }, testInfo) => {
    const isMobile = testInfo.project.use?.isMobile ?? false;
    const viewport = testInfo.project.use?.viewport;
    const context = await browser.newContext({ baseURL: testConfig.baseUrl, viewport });

    try {
      const page = await context.newPage();
      await page.goto("/");

      const authPage = new AuthPage(page, isMobile);
      const minersPage = new MinersPage(page, isMobile);
      const settingsCurtailmentPage = new SettingsCurtailmentPage(page, isMobile);
      const commonSteps = new CommonSteps(authPage, minersPage);

      await commonSteps.loginAsAdmin();
      await settingsCurtailmentPage.navigateToCurtailmentSettings();
      await settingsCurtailmentPage.deleteSourcesByPrefix(SOURCE_PREFIX);
      await settingsCurtailmentPage.deleteResponseProfilesByPrefix(RESPONSE_PROFILE_PREFIX);
    } finally {
      await context.close();
    }
  });

  test("Create and delete curtailment response profiles and sources", async ({
    commonSteps,
    page,
    settingsCurtailmentPage,
  }) => {
    const responseProfileName = generateRandomText(RESPONSE_PROFILE_PREFIX);
    const sourceName = generateRandomText(SOURCE_PREFIX);
    const sourceInput = {
      name: sourceName,
      brokerPrimaryHost: "127.0.0.1",
      brokerSecondaryHost: "127.0.0.2",
      brokerPort: "1883",
      topic: `curtailment/e2e/${sourceName}/target`,
      username: "curtailment-e2e",
      password: "curtailment-e2e-password",
    };

    await test.step("Log in as admin", async () => {
      await commonSteps.loginAsAdmin();
    });

    await test.step("Navigate to curtailment settings", async () => {
      await settingsCurtailmentPage.navigateToCurtailmentSettings();
      await settingsCurtailmentPage.validateCurtailmentPageOpened();
    });

    let createProfileRequest!: Awaited<ReturnType<typeof page.waitForRequest>>;

    await test.step("Create a whole-fleet response profile", async () => {
      await settingsCurtailmentPage.openCreateResponseProfile();
      await settingsCurtailmentPage.fillResponseProfile({
        name: responseProfileName,
        curtailBatchSize: "25",
        curtailBatchIntervalSec: "60",
        restoreBatchSize: "10",
        restoreBatchIntervalSec: "120",
      });

      [createProfileRequest] = await Promise.all([
        page.waitForRequest(/CreateCurtailmentResponseProfile/),
        settingsCurtailmentPage.saveResponseProfile(),
      ]);
    });

    await test.step("Validate the response profile payload and card", async () => {
      const requestBody = createProfileRequest.postDataJSON() as {
        profileName?: string;
        mode?: string;
        strategy?: string;
        level?: string;
        priority?: string;
        curtailBatchSize?: number;
        curtailBatchIntervalSec?: number;
        restoreBatchSize?: number;
        restoreBatchIntervalSec?: number;
        includeMaintenance?: boolean;
        forceIncludeMaintenance?: boolean;
      };

      expect(createProfileRequest.method()).toBe("POST");
      expect(requestBody.profileName).toBe(responseProfileName);
      expect(requestBody.mode).toBe("CURTAILMENT_MODE_FULL_FLEET");
      expect(requestBody.strategy).toBe("CURTAILMENT_STRATEGY_LEAST_EFFICIENT_FIRST");
      expect(requestBody.level).toBe("CURTAILMENT_LEVEL_FULL");
      expect(requestBody.priority).toBe("CURTAILMENT_PRIORITY_NORMAL");
      expect(requestBody.curtailBatchSize).toBe(25);
      expect(requestBody.curtailBatchIntervalSec).toBe(60);
      expect(requestBody.restoreBatchSize).toBe(10);
      expect(requestBody.restoreBatchIntervalSec).toBe(120);
      expect(requestBody.includeMaintenance).toBe(true);
      expect(requestBody.forceIncludeMaintenance).toBe(true);
      await settingsCurtailmentPage.validateResponseProfileVisible(responseProfileName);
    });

    let createSourceRequest!: Awaited<ReturnType<typeof page.waitForRequest>>;

    await test.step("Create an MQTT curtailment source", async () => {
      await settingsCurtailmentPage.openAddSource();
      await settingsCurtailmentPage.fillSource(sourceInput);

      [createSourceRequest] = await Promise.all([
        page.waitForRequest(/CreateMqttCurtailmentSource/),
        settingsCurtailmentPage.saveSource(),
      ]);
    });

    await test.step("Validate the source payload and row", async () => {
      const requestBody = createSourceRequest.postDataJSON() as {
        sourceName?: string;
        topic?: string;
        brokerPrimaryHost?: string;
        brokerSecondaryHost?: string;
        brokerPort?: number;
        brokerTransport?: string;
        mqttUsername?: string;
        mqttPassword?: string;
        payloadFormat?: string;
        stalenessThresholdSec?: number;
      };

      expect(createSourceRequest.method()).toBe("POST");
      expect(requestBody.sourceName).toBe(sourceName);
      expect(requestBody.topic).toBe(sourceInput.topic);
      expect(requestBody.brokerPrimaryHost).toBe(sourceInput.brokerPrimaryHost);
      expect(requestBody.brokerSecondaryHost).toBe(sourceInput.brokerSecondaryHost);
      expect(requestBody.brokerPort).toBe(Number(sourceInput.brokerPort));
      expect(requestBody.brokerTransport).toBe("tcp");
      expect(requestBody.mqttUsername).toBe(sourceInput.username);
      expect(requestBody.mqttPassword).toBe(sourceInput.password);
      expect(requestBody.payloadFormat).toBe("target_timestamp");
      expect(requestBody.stalenessThresholdSec).toBe(240);
      await settingsCurtailmentPage.validateSourceVisible(sourceName);
    });

    await test.step("Delete the created curtailment settings", async () => {
      await settingsCurtailmentPage.deleteSourcesByPrefix(SOURCE_PREFIX);
      await settingsCurtailmentPage.deleteResponseProfilesByPrefix(RESPONSE_PROFILE_PREFIX);
    });
  });
});
