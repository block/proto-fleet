import type { APIRequestContext, Page } from "@playwright/test";
import { test } from "../fixtures/pageFixtures";
import type { MiningTarget, MiningTargetResponse } from "@/protoOS/api/generatedApi";

function chooseCustomPowerTargetWatts({
  currentTargetWatts,
  defaultTargetWatts,
  minTargetWatts,
  maxTargetWatts,
}: {
  currentTargetWatts?: number;
  defaultTargetWatts?: number;
  minTargetWatts?: number;
  maxTargetWatts?: number;
}) {
  if (
    currentTargetWatts === undefined ||
    defaultTargetWatts === undefined ||
    minTargetWatts === undefined ||
    maxTargetWatts === undefined
  ) {
    throw new Error("Missing mining target bounds from API.");
  }

  const candidates = [
    defaultTargetWatts + 100,
    defaultTargetWatts - 100,
    minTargetWatts + 100,
    maxTargetWatts - 100,
    Math.round((minTargetWatts + maxTargetWatts) / 200) * 100,
  ];

  const validCandidate = candidates.find(
    (candidate) =>
      candidate >= minTargetWatts &&
      candidate <= maxTargetWatts &&
      candidate !== currentTargetWatts &&
      candidate !== minTargetWatts &&
      candidate !== defaultTargetWatts &&
      candidate !== maxTargetWatts,
  );

  if (validCandidate === undefined) {
    throw new Error(
      `Could not find a distinct custom power target. Current=${currentTargetWatts}, default=${defaultTargetWatts}, min=${minTargetWatts}, max=${maxTargetWatts}`,
    );
  }

  return validCandidate;
}

function formatPowerTargetWidgetText(targetWatts: number) {
  return `${targetWatts / 1000} kW custom target`;
}

async function getAuthAccessToken(page: Page) {
  return page.evaluate(() => {
    const authData = window.localStorage.getItem("proto-os-auth");
    if (!authData) {
      throw new Error("Missing proto-os-auth in localStorage");
    }

    const parsed = JSON.parse(authData) as {
      state?: {
        auth?: {
          authTokens?: {
            accessToken?: { value?: string };
          };
        };
      };
    };

    const accessToken = parsed.state?.auth?.authTokens?.accessToken?.value;
    if (!accessToken) {
      throw new Error("Missing access token in proto-os-auth");
    }

    return accessToken;
  });
}

async function waitForAuthenticatedMiningTarget(request: APIRequestContext, page: Page) {
  await test.expect
    .poll(
      async () => {
        try {
          const accessToken = await getAuthAccessToken(page);
          const response = await request.get("/api/v1/mining/target", {
            headers: {
              Authorization: `Bearer ${accessToken}`,
            },
          });

          return response.status();
        } catch {
          return 0;
        }
      },
      { timeout: 10_000 },
    )
    .toBe(200);
}

async function authenticateAsAdminAndWaitForAccess({
  page,
  commonSteps,
  request,
}: {
  page: Page;
  commonSteps: { authenticateAsAdmin: () => Promise<void> };
  request: APIRequestContext;
}) {
  let lastError: unknown;

  for (let attempt = 0; attempt < 2; attempt += 1) {
    try {
      await commonSteps.authenticateAsAdmin();
      await waitForAuthenticatedMiningTarget(request, page);
      return;
    } catch (error) {
      lastError = error;

      if (attempt === 1) {
        throw error;
      }

      await page.goto("/");
    }
  }

  throw lastError;
}

async function getMiningTargetState(request: APIRequestContext, getAccessToken: () => Promise<string>) {
  const accessToken = await getAccessToken();
  const response = await request.get("/api/v1/mining/target", {
    headers: {
      Authorization: `Bearer ${accessToken}`,
    },
  });

  test.expect(response.status()).toBe(200);

  const responseBody = (await response.json()) as MiningTargetResponse;

  return {
    defaultTargetWatts: responseBody.default_power_target_watts,
    minTargetWatts: responseBody.power_target_min_watts,
    maxTargetWatts: responseBody.power_target_max_watts,
    currentTargetWatts: responseBody.power_target_watts,
    performanceMode: responseBody.performance_mode,
  };
}

async function restoreMiningTargetState({
  request,
  accessToken,
  miningTargetState,
}: {
  request: APIRequestContext;
  accessToken: string;
  miningTargetState: { currentTargetWatts?: number; performanceMode?: MiningTarget["performance_mode"] };
}) {
  const response = await request.put("/api/v1/mining/target", {
    headers: {
      Authorization: `Bearer ${accessToken}`,
    },
    data: {
      performance_mode: miningTargetState.performanceMode,
      power_target_watts: miningTargetState.currentTargetWatts,
    } satisfies MiningTarget,
  });

  test.expect(response.status()).toBe(200);
}

test.describe("ProtoOS header actions and power target", () => {
  test.beforeEach(async ({ page, commonSteps, request }) => {
    await page.goto("/");
    await authenticateAsAdminAndWaitForAccess({ page, commonSteps, request });
  });

  test("Blink LEDs from header actions sends locate request", async ({ headerComponent, page }) => {
    const requestPromise = page.waitForRequest(
      (request) => request.method() === "POST" && request.url().includes("/api/v1/system/locate"),
    );
    const responsePromise = page.waitForResponse(
      (response) => response.request().method() === "POST" && response.url().includes("/api/v1/system/locate"),
    );

    await test.step("Trigger Blink LEDs from header actions", async () => {
      await headerComponent.openGlobalActionsMenu();
      await headerComponent.clickBlinkLeds();
      await headerComponent.validateGlobalActionsMenuClosed();
    });

    await test.step("Validate locate request", async () => {
      const request = await requestPromise;
      const response = await responsePromise;
      const requestUrl = new URL(request.url());

      test.expect(requestUrl.searchParams.get("led_on_time")).toBe("30");
      test.expect(response.status()).toBe(202);
    });
  });

  test("Download logs from header actions starts a CSV download", async ({ headerComponent, page }) => {
    await test.step("Trigger download from header actions", async () => {
      const downloadPromise = page.waitForEvent("download");

      await headerComponent.openGlobalActionsMenu();
      await headerComponent.clickDownloadLogs();
      await headerComponent.validateGlobalActionsMenuClosed();

      const download = await downloadPromise;
      const fileName = download.suggestedFilename();

      test.expect(fileName.startsWith("miner-logs-")).toBe(true);
      test.expect(fileName.endsWith(".csv")).toBe(true);
    });
  });

  test("Power target custom value applies and persists after reload", async ({ headerComponent, page, request }) => {
    const accessToken = await getAuthAccessToken(page);
    const miningTargetState = await getMiningTargetState(request, () => Promise.resolve(accessToken));
    const customTargetWatts = chooseCustomPowerTargetWatts(miningTargetState);
    const customTargetKw = customTargetWatts / 1000;

    try {
      const requestPromise = page.waitForRequest(
        (request) => request.method() === "PUT" && request.url().includes("/api/v1/mining/target"),
      );
      const responsePromise = page.waitForResponse(
        (response) => response.request().method() === "PUT" && response.url().includes("/api/v1/mining/target"),
      );

      await test.step("Apply a custom power target", async () => {
        await headerComponent.openPowerTargetPopover();
        await headerComponent.clickCustomPowerTargetMode();
        await headerComponent.inputCustomPowerTargetKw(customTargetKw);
        await headerComponent.clickApplyPowerTarget();
      });

      await test.step("Validate the update request and widget text", async () => {
        const request = await requestPromise;
        const response = await responsePromise;
        const requestBody = request.postDataJSON();

        test.expect(requestBody.power_target_watts).toBe(customTargetWatts);
        test.expect(response.status()).toBe(200);
        await headerComponent.validatePowerTargetWidgetText(formatPowerTargetWidgetText(customTargetWatts));
      });

      await test.step("Reload and validate the custom target persists", async () => {
        await page.reload();
        await headerComponent.validatePowerTargetWidgetText(formatPowerTargetWidgetText(customTargetWatts));
      });
    } finally {
      await restoreMiningTargetState({
        request,
        accessToken,
        miningTargetState,
      });
    }
  });
});
