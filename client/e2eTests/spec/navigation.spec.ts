/* eslint-disable playwright/expect-expect */
import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";

test.describe.serial("Navigation", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test("Overview navigation", async ({ authPage, homePage, minersPage }) => {
    await test.step("Login as admin", async () => {
      await authPage.inputUsername(testConfig.users.admin.username);
      await authPage.inputPassword(testConfig.users.admin.password);
      await authPage.clickLogin();
      await authPage.validateLoggedIn();
    });

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
});
