import type { Page, Request, TestInfo } from "@playwright/test";
import { testConfig } from "../config/test.config";
import { expect, test } from "../fixtures/pageFixtures";
import { CommonSteps } from "../helpers/commonSteps";
import { generateRandomText } from "../helpers/testDataHelper";
import { AuthPage } from "../pages/auth";
import { MinersPage } from "../pages/miners";
import { SettingsCurtailmentPage } from "../pages/settingsCurtailment";

const RESPONSE_PROFILE_BASE_PREFIX = "curtailment_profile_e2e";
const SOURCE_BASE_PREFIX = "curtailment_source_e2e";

type CurtailmentRunPrefixes = {
  responseProfilePrefix: string;
  sourcePrefix: string;
};

type CreateResponseProfileRequestBody = {
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

type CreateSourceRequestBody = {
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

const testRunPrefixes = new Map<string, CurtailmentRunPrefixes>();

function getTestRunKey(testInfo: TestInfo): string {
  return [testInfo.project.name, testInfo.testId, testInfo.workerIndex, testInfo.retry, testInfo.repeatEachIndex].join(
    ":",
  );
}

function getSafeProjectName(testInfo: TestInfo): string {
  return (
    testInfo.project.name
      .replace(/[^a-zA-Z0-9]+/g, "_")
      .replace(/^_+|_+$/g, "")
      .toLowerCase() || "project"
  );
}

function createRunPrefixes(testInfo: TestInfo): CurtailmentRunPrefixes {
  const runPrefix = `${getSafeProjectName(testInfo)}_${testInfo.workerIndex}_${testInfo.retry}`;

  return {
    responseProfilePrefix: generateRandomText(`${RESPONSE_PROFILE_BASE_PREFIX}_${runPrefix}`),
    sourcePrefix: generateRandomText(`${SOURCE_BASE_PREFIX}_${runPrefix}`),
  };
}

function getRunPrefixes(testInfo: TestInfo): CurtailmentRunPrefixes {
  const prefixes = testRunPrefixes.get(getTestRunKey(testInfo));
  if (!prefixes) {
    throw new Error("Curtailment E2E run prefixes were not initialized.");
  }

  return prefixes;
}

function getRequestBody(request: Request): Record<string, unknown> {
  try {
    const requestBody = request.postDataJSON();
    return typeof requestBody === "object" && requestBody !== null ? (requestBody as Record<string, unknown>) : {};
  } catch {
    return {};
  }
}

function isCreateResponseProfileRequest(request: Request, profileName: string): boolean {
  if (request.method() !== "POST" || !request.url().includes("CreateCurtailmentResponseProfile")) {
    return false;
  }

  return getRequestBody(request).profileName === profileName;
}

function isCreateSourceRequest(request: Request, sourceName: string): boolean {
  if (request.method() !== "POST" || !request.url().includes("CreateMqttCurtailmentSource")) {
    return false;
  }

  return getRequestBody(request).sourceName === sourceName;
}

async function deleteCurtailmentSettingsCreatedForRun(page: Page, isMobile: boolean, prefixes: CurtailmentRunPrefixes) {
  await page.goto("/");

  const authPage = new AuthPage(page, isMobile);
  const minersPage = new MinersPage(page, isMobile);
  const settingsCurtailmentPage = new SettingsCurtailmentPage(page, isMobile);
  const commonSteps = new CommonSteps(authPage, minersPage);

  await commonSteps.loginAsAdmin();
  await settingsCurtailmentPage.navigateToCurtailmentSettings();
  await settingsCurtailmentPage.deleteSourcesByPrefix(prefixes.sourcePrefix);
  await settingsCurtailmentPage.deleteResponseProfilesByPrefix(prefixes.responseProfilePrefix);
}

test.describe("Proto Fleet - Curtailment Settings", () => {
  test.beforeEach(async ({ page }, testInfo) => {
    testRunPrefixes.set(getTestRunKey(testInfo), createRunPrefixes(testInfo));
    await page.goto("/");
  });

  test.afterEach("CLEANUP: Delete curtailment settings created during tests", async ({ browser, page }, testInfo) => {
    const isMobile = testInfo.project.use?.isMobile ?? false;
    const viewport = testInfo.project.use?.viewport;
    const runKey = getTestRunKey(testInfo);
    const prefixes = testRunPrefixes.get(runKey);

    if (!prefixes) {
      return;
    }

    try {
      if (!page.isClosed()) {
        await deleteCurtailmentSettingsCreatedForRun(page, isMobile, prefixes);
        return;
      }

      const context = await browser.newContext({ baseURL: testConfig.baseUrl, viewport });
      try {
        const cleanupPage = await context.newPage();
        await deleteCurtailmentSettingsCreatedForRun(cleanupPage, isMobile, prefixes);
      } finally {
        await context.close();
      }
    } finally {
      testRunPrefixes.delete(runKey);
    }
  });

  test("Create curtailment response profiles and sources", async ({
    commonSteps,
    page,
    settingsCurtailmentPage,
  }, testInfo) => {
    const { responseProfilePrefix, sourcePrefix } = getRunPrefixes(testInfo);
    const responseProfileName = generateRandomText(responseProfilePrefix);
    const sourceName = generateRandomText(sourcePrefix);
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
        page.waitForRequest((request) => isCreateResponseProfileRequest(request, responseProfileName)),
        settingsCurtailmentPage.saveResponseProfile(),
      ]);
    });

    await test.step("Validate the response profile payload and card", async () => {
      const requestBody = createProfileRequest.postDataJSON() as CreateResponseProfileRequestBody;

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
        page.waitForRequest((request) => isCreateSourceRequest(request, sourceName)),
        settingsCurtailmentPage.saveSource(),
      ]);
    });

    await test.step("Validate the source payload and row", async () => {
      const requestBody = createSourceRequest.postDataJSON() as CreateSourceRequestBody;

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
  });
});
