/* eslint-disable playwright/expect-expect */
import { testConfig } from "../config/test.config";
import { expect, test } from "../fixtures/pageFixtures";

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

    await test.step("Start authentication process", async () => {
      await homePage.validateCompleteSetupTitle();
      await homePage.clickAuthenticateMinersButton();
      await homePage.validateAuthenticateMinersModalTitle();
    });

    await test.step("Validate 4 miners need authentication - S17, S19, S19, S21", async () => {
      await homePage.validateTextInModal("Bulk authenticate");
      await homePage.validateTextInModal("4 miners remaining");
      await homePage.clickShowMinersButton();
      await homePage.validateTextInModal("Bulk authenticate");
      await homePage.validateTextInModal("4 miners remaining");
      const miners = await homePage.getListOfMinersToAuthenticate();
      expect(miners).toHaveLength(4);
      expect(miners).toContain("Antminer S21 XP");
      expect(miners).toContain("Antminer S17 XP");
      expect(miners.filter((model) => model === "Antminer S19 XP")).toHaveLength(2);
    });

    await test.step("Bulk authenticate all miners with S19 credentials", async () => {
      await homePage.inputMinerAuthUsername("root19");
      await homePage.inputMinerAuthPassword("root19");
      await homePage.clickAuthenticateMinersConfirmButton();
    });

    await test.step("Validate S19 miners authenticated, but S21 and S17 not", async () => {
      await homePage.validateTextInToast("You authenticated 2 of 4 miners.");
      await homePage.validateCalloutInModal("Try your username and password again.");
      await homePage.clickCalloutButton();
      const miners = await homePage.getListOfMinersToAuthenticate();
      expect(miners).toHaveLength(2);
      expect(miners).toContain("Antminer S21 XP");
      expect(miners).toContain("Antminer S17 XP");
    });

    await test.step("Try authenticating S21 miner incorrectly with S17 miner's credentials", async () => {
      await homePage.clickMinerAuthCheckbox("Antminer S17 XP");
      await homePage.inputMinerRowUsername("Antminer S21 XP", "root17");
      await homePage.inputMinerRowPassword("Antminer S21 XP", "root17");
      await homePage.clickAuthenticateMinersConfirmButton();
    });

    await test.step("Validate S21 miner's authentication failed", async () => {
      await homePage.validateTextInToast("Authentication failed. Please check your credentials and try again.");
      await homePage.validateCalloutInModal("Try your username and password again.");
      await homePage.clickCalloutButton();
    });

    await test.step("Authenticating S21 miner", async () => {
      await homePage.inputMinerRowUsername("Antminer S21 XP", "root21");
      await homePage.inputMinerRowPassword("Antminer S21 XP", "root21");
      await homePage.clickAuthenticateMinersConfirmButton();
    });

    await test.step("Validate S21 miner successfully authenticated", async () => {
      await homePage.validateTextInToast("1 miner authenticated.");
      await homePage.validateNoCalloutInModal();
    });

    await test.step("Bulk authenticate last miner - S17", async () => {
      await homePage.clickMinerAuthCheckbox("Antminer S17 XP");
      await homePage.inputMinerAuthUsername("root17");
      await homePage.inputMinerAuthPassword("root17");
      await homePage.clickAuthenticateMinersConfirmButton();
    });

    await test.step("Validate all miners authenticated", async () => {
      await homePage.validateTextInToast("All miners authenticated.");
      await homePage.validateModalClosed();
      await homePage.validateAuthenticateMinersButtonNotVisible();
    });
  });
});
