/* eslint-disable playwright/expect-expect */
import { test } from "../fixtures/pageFixtures";

test.describe("Navigation", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test("Overview navigation", async ({ homePage, minersPage, commonSteps }) => {
    await commonSteps.loginAsAdmin();

    await test.step("Navigate to control board issues", async () => {
      await homePage.clickControlBoardsLink();
      await minersPage.validateMinersPageOpened();
      await minersPage.validateActiveFilter("Control board issue");
    });

    await test.step("Navigate back to overview", async () => {
      await homePage.navigateToHomePage();
      await homePage.validateHomePageOpened();
    });

    await test.step("Navigate to fan issues", async () => {
      await homePage.clickFansLink();
      await minersPage.validateMinersPageOpened();
      await minersPage.validateActiveFilter("Fan issue");
    });

    await test.step("Navigate back to overview", async () => {
      await homePage.navigateToHomePage();
      await homePage.validateHomePageOpened();
    });

    await test.step("Navigate to hashboard issues", async () => {
      await homePage.clickHashboardsLink();
      await minersPage.validateMinersPageOpened();
      await minersPage.validateActiveFilter("Hash board issue");
    });

    await test.step("Navigate back to overview", async () => {
      await homePage.navigateToHomePage();
      await homePage.validateHomePageOpened();
    });

    await test.step("Navigate to power supply issues", async () => {
      await homePage.clickPowerSuppliesLink();
      await minersPage.validateMinersPageOpened();
      await minersPage.validateActiveFilter("PSU issue");
    });

    await test.step("Navigate back to overview", async () => {
      await homePage.navigateToHomePage();
      await homePage.validateHomePageOpened();
    });
  });

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
