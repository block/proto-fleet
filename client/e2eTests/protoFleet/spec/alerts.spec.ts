import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";
import { CommonSteps } from "../helpers/commonSteps";
import { generateRandomText } from "../helpers/testDataHelper";
import { AlertsPage } from "../pages/alerts";
import { AuthPage } from "../pages/auth";
import { MinersPage } from "../pages/miners";

const CHANNEL_PREFIX = "e2e-webhook";

// Alerts are a flagged beta that needs the Grafana sidecar (`just dev-alerts`)
// and a client built with VITE_ALERTS_ENABLED. The default CI E2E stack has
// neither, so this spec runs only when the env opts in via E2E_ALERTS_ENABLED;
// the server unit tests are the CI regression guard for the test-channel path.
const ALERTS_E2E_ENABLED = process.env.E2E_ALERTS_ENABLED === "true";

// A webhook destination Grafana can actually reach from the alerts dev
// stack and that answers 2xx, so a "Test" reports successful delivery. The
// alerts overlay (`just dev-alerts`) runs grafana + otel-collector on the
// same network and allows private destinations, so the otel health endpoint is a
// stable in-network sink. If a test only reached the dead /provisioning test
// route (the bug this spec guards), Grafana would 404 and delivery would fail.
const REACHABLE_WEBHOOK_URL = "http://otel-collector:13133/healthz";

test.describe("Proto Fleet - Alerts", () => {
  let shouldCleanupChannels = false;

  // eslint-disable-next-line playwright/no-skipped-test
  test.skip(
    !ALERTS_E2E_ENABLED,
    "Requires the alerts sidecar + VITE_ALERTS_ENABLED; set E2E_ALERTS_ENABLED=true to run.",
  );

  test.beforeEach(async ({ page }) => {
    shouldCleanupChannels = false;
    await page.goto("/");
  });

  test.afterEach("CLEANUP: delete channels created during tests", async ({ browser }, testInfo) => {
    if (!shouldCleanupChannels) {
      return;
    }

    const isMobile = testInfo.project.use?.isMobile ?? false;
    const viewport = testInfo.project.use?.viewport;
    const context = await browser.newContext({ baseURL: testConfig.baseUrl, viewport });

    try {
      const page = await context.newPage();
      await page.goto("/");

      const authPage = new AuthPage(page, isMobile);
      const minersPage = new MinersPage(page, isMobile);
      const alertsPage = new AlertsPage(page, isMobile);
      const commonSteps = new CommonSteps(authPage, minersPage);

      await commonSteps.loginAsAdmin();
      await alertsPage.navigateToAlertsSettings();
      await alertsPage.deleteChannelsByPrefix(CHANNEL_PREFIX);
    } finally {
      await context.close();
    }
  });

  test("Webhook contact point can be tested through Grafana", async ({ commonSteps, alertsPage }) => {
    const channelName = generateRandomText(CHANNEL_PREFIX);

    await test.step("Log in as admin", async () => {
      await commonSteps.loginAsAdmin();
    });

    await test.step("Navigate to Alerts settings", async () => {
      await alertsPage.navigateToAlertsSettings();
    });

    await test.step("Test a webhook destination before saving", async () => {
      await alertsPage.openAddChannelModal();
      await alertsPage.fillWebhookChannel(channelName, REACHABLE_WEBHOOK_URL);
      await alertsPage.sendTestFromModal();
      // Pre-fix this hit Grafana's removed /provisioning test route and always
      // failed; a real delivery now returns a success toast.
      await alertsPage.validateTextInToast("Test delivered");
    });

    await test.step("Save the channel", async () => {
      shouldCleanupChannels = true;
      await alertsPage.saveChannel();
      await alertsPage.validateChannelListed(channelName);
      await alertsPage.validateChannelStatus(channelName, "Not tested");
    });

    await test.step("Test the saved channel from its row action", async () => {
      await alertsPage.testSavedChannel(channelName);
      await alertsPage.validateTextInToast("Test delivery sent");
      // A successful test must flip the row status badge, not leave it "Not tested".
      await alertsPage.validateChannelStatus(channelName, "Validated");
    });

    await test.step("Delete the channel", async () => {
      await alertsPage.deleteChannel(channelName);
      shouldCleanupChannels = false;
    });
  });
});
