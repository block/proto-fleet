/* eslint-disable playwright/expect-expect */
import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";

test.describe("Proto Fleet - Onboarding", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test("Onboard the admin user @setup", async ({ authPage }) => {
    await test.step("Create credentials", async () => {
      await authPage.inputUsername(testConfig.users.admin.username);
      await authPage.inputPassword(testConfig.users.admin.password);
      await authPage.clickContinue();
    });

    await test.step("Validate admin is logged in", async () => {
      await authPage.validateLoggedIn();
    });
  });

  test("Validate null states", async ({ homePage, commonSteps, minersPage, settingsPoolsPage }) => {
    await commonSteps.loginAsAdmin();

    await test.step("Validate Home screen null state due to no miners added", async () => {
      await homePage.validateTextIsVisible("Let's setup your fleet.");
      await homePage.validateTextIsVisible("Add miners to your fleet to get started.");
      await homePage.validateButtonIsVisible("Get Started");
    });

    await test.step("Validate Miners screen null state due to no miners added", async () => {
      await homePage.navigateToMinersPage();
      await minersPage.validateTextIsVisible("You haven't paired any miners");
      await minersPage.validateTextIsVisible("Add miners to your fleet to get started.");
      await minersPage.validateButtonIsVisible("Get Started");
    });

    await test.step("Validate Pools screen null state due to no pools added", async () => {
      await minersPage.navigateToMiningPoolsSettings();
      await settingsPoolsPage.validateTitle("Pools");
      await settingsPoolsPage.validateTextIsVisible("Add a pool to start assigning your miners.");
      await settingsPoolsPage.validateButtonIsVisible("Add pool");
    });
  });

  test("Add all scanned miners @setup", async ({ authPage, minersPage, commonSteps }) => {
    await commonSteps.loginAsAdmin();

    await test.step("Get started with onboarding", async () => {
      await authPage.clickGetStarted();
    });

    await test.step("Find and add miners", async () => {
      await authPage.clickFindMiners();
      await authPage.clickContinueWithSelectedMiners();
    });

    await commonSteps.goToMinersPage();

    await test.step("Validate miners added", async () => {
      await minersPage.validateMinersAdded();
    });
  });

  test("Authenticate miners @setup", async ({ homePage, commonSteps }) => {
    await commonSteps.loginAsAdmin();

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
    });
  });
});
