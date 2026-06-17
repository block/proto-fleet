import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";
import { CommonSteps } from "../helpers/commonSteps";
import { generateRandomText } from "../helpers/testDataHelper";
import { AuthPage } from "../pages/auth";
import { MinersPage } from "../pages/miners";
import { NotificationsPage } from "../pages/notifications";

const CHANNEL_PREFIX = "e2e-webhook";

// A webhook destination Grafana can actually reach from the notifications dev
// stack and that answers 2xx, so a "Test" reports successful delivery. The
// notifications overlay (`just dev-notifs`) runs grafana + otel-collector on the
// same network and allows private destinations, so the otel health endpoint is a
// stable in-network sink. If a test only reached the dead /provisioning test
// route (the bug this spec guards), Grafana would 404 and delivery would fail.
const REACHABLE_WEBHOOK_URL = "http://otel-collector:13133/healthz";

test.describe("Proto Fleet - Notifications", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test.afterEach("CLEANUP: delete channels created during tests", async ({ browser }, testInfo) => {
    const isMobile = testInfo.project.use?.isMobile ?? false;
    const viewport = testInfo.project.use?.viewport;
    const context = await browser.newContext({ baseURL: testConfig.baseUrl, viewport });

    try {
      const page = await context.newPage();
      await page.goto("/");

      const authPage = new AuthPage(page, isMobile);
      const minersPage = new MinersPage(page, isMobile);
      const notificationsPage = new NotificationsPage(page, isMobile);
      const commonSteps = new CommonSteps(authPage, minersPage);

      await commonSteps.loginAsAdmin();
      await notificationsPage.navigateToNotificationsSettings();
      await notificationsPage.deleteChannelsByPrefix(CHANNEL_PREFIX);
    } finally {
      await context.close();
    }
  });

  test("Webhook contact point can be tested through Grafana", async ({ commonSteps, notificationsPage }) => {
    const channelName = generateRandomText(CHANNEL_PREFIX);

    await test.step("Log in as admin", async () => {
      await commonSteps.loginAsAdmin();
    });

    await test.step("Navigate to Notifications settings", async () => {
      await notificationsPage.navigateToNotificationsSettings();
    });

    await test.step("Test a webhook destination before saving", async () => {
      await notificationsPage.openAddChannelModal();
      await notificationsPage.fillWebhookChannel(channelName, REACHABLE_WEBHOOK_URL);
      await notificationsPage.sendTestFromModal();
      // Pre-fix this hit Grafana's removed /provisioning test route and always
      // failed; a real delivery now returns a success toast.
      await notificationsPage.validateTextInToast("Test delivered");
    });

    await test.step("Save the channel", async () => {
      await notificationsPage.saveChannel();
      await notificationsPage.validateChannelListed(channelName);
    });

    await test.step("Test the saved channel from its row action", async () => {
      await notificationsPage.testSavedChannel(channelName);
      await notificationsPage.validateTextInToast("Test delivery sent");
    });

    await test.step("Delete the channel", async () => {
      await notificationsPage.deleteChannel(channelName);
    });
  });
});
