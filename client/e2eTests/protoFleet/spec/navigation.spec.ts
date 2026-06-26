import { test } from "../fixtures/pageFixtures";

test.describe("Navigation", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  // NOTE: the former "Overview navigation" test exercised the component-issue
  // cards (Control Boards / Fans / Hashboards / Power supplies) on the all-sites
  // dashboard. The dashboard redesign moved that breakdown to the single-site
  // Fleet health "Components" tab, so the all-sites affordance no longer exists.
  // Fresh coverage for the Components-tab navigation is tracked as follow-up.

  test("Navigate between main pages and settings sub-pages", async ({ authPage, commonSteps, settingsPage }) => {
    await commonSteps.loginAsAdmin();

    await test.step("Navigate from Home to Settings page", async () => {
      await authPage.navigateToSettingsPage();
    });

    await test.step("Navigate from Settings to Team Settings", async () => {
      await settingsPage.navigateToTeamSettings();
    });

    await test.step("Navigate from Team Settings back to Settings page", async () => {
      await settingsPage.navigateToSettingsPage();
    });

    await test.step("Navigate from Settings to Home page", async () => {
      await settingsPage.navigateToHomePage();
    });

    await test.step("Navigate from Home to Team Settings", async () => {
      await settingsPage.navigateToTeamSettings();
    });

    await test.step("Navigate from Team Settings back to Settings page", async () => {
      await settingsPage.navigateToSettingsPage();
    });

    await test.step("Navigate from Settings to Security Settings", async () => {
      await settingsPage.navigateToSecuritySettings();
    });

    await test.step("Navigate from Security Settings to Mining Pools Settings", async () => {
      await settingsPage.navigateToMiningPoolsSettings();
    });

    await test.step("Navigate from Mining Pools Settings to Miners page", async () => {
      await settingsPage.navigateToMinersPage();
    });

    await test.step("Navigate from Miners page back to Mining Pools Settings", async () => {
      await settingsPage.navigateToMiningPoolsSettings();
    });

    await test.step("Navigate from Mining Pools Settings to Home page", async () => {
      await settingsPage.navigateToHomePage();
    });
  });
});
