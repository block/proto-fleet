/* eslint-disable playwright/expect-expect */
import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";

test.describe("Proto Fleet - Onboarding", () => {
  test.describe.configure({ mode: "serial" });

  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test("Onboard the admin user", async ({ authPage }) => {
    await test.step("Create credentials", async () => {
      await authPage.inputUsername(testConfig.users.admin.username);
      await authPage.inputPassword(testConfig.users.admin.password);
      await authPage.clickContinue();
    });

    await test.step("Validate admin is logged in", async () => {
      await authPage.validateLoggedIn();
    });
  });

  test("Add all scanned miners", async ({ authPage, minersPage }) => {
    await test.step("Login as admin", async () => {
      await authPage.inputUsername(testConfig.users.admin.username);
      await authPage.inputPassword(testConfig.users.admin.password);
      await authPage.clickLogin();
      await authPage.validateLoggedIn();
    });

    await test.step("Get started with onboarding", async () => {
      await authPage.clickGetStarted();
    });

    await test.step("Find and add miners", async () => {
      await authPage.clickFindMiners();
      await authPage.clickContinueWithSelectedMiners();
    });

    await test.step("Navigate to miners page and validate", async () => {
      await authPage.navigateToMinersPage();
      await minersPage.waitForMinersTitle();
      await minersPage.waitForMinersListToLoad();
      await minersPage.validateMinersAdded();
    });
  });

  test("Authenticate miners", async ({ authPage, homePage, minersPage }) => {
    await test.step("Login as admin", async () => {
      await authPage.inputUsername(testConfig.users.admin.username);
      await authPage.inputPassword(testConfig.users.admin.password);
      await authPage.clickLogin();
      await authPage.validateLoggedIn();
    });

    await test.step("Authenticate miners", async () => {
      await homePage.validateCompleteSetupTitle();
      await homePage.clickAuthenticateMinersButton();
      await homePage.validateAuthenticateMinersModalTitle();
      await homePage.inputMinerAuthUsername(testConfig.miners.username);
      await homePage.inputMinerAuthPassword(testConfig.miners.password);
      await homePage.clickAuthenticateMinersConfirmButton();
      await homePage.validateCompleteSetupTitleNotVisible();
      await homePage.validateAuthenticateMinersButtonNotVisible();
    });

    await test.step("Validate all miners authenticated", async () => {
      await homePage.validateCompleteSetupTitleNotVisible();
      await homePage.validateAuthenticateMinersButtonNotVisible();
      await authPage.navigateToMinersPage();
      await minersPage.validateAllMinersStatus("Hashing");
    });
  });
});
