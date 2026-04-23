/* eslint-disable playwright/expect-expect */
import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";
import { generateRandomText } from "../helpers/testDataHelper";

test.describe("Onboarding", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test("Complete onboarding flow @setup", async ({ welcomePage, homePage, poolsPage: poolsPage }) => {
    const poolName = generateRandomText("PoolName");
    const poolUsername = generateRandomText("PoolUsername");
    const poolPassword = generateRandomText("PoolPassword");

    await test.step("Welcome screen - validate and start setup", async () => {
      await welcomePage.validateWelcomeUrl();
      await welcomePage.validateTextIsVisible("Miner setup");
      await welcomePage.clickGetStartedButton();
    });

    await test.step("Verify miner - validate information and continue", async () => {
      await welcomePage.validateVerifyUrl();
      await welcomePage.validateTitle("Is this the miner you want to set up?");
      await welcomePage.validateTextIsVisible("Controller Serial");
      await welcomePage.validateTextIsVisible("Mac Address");
      await welcomePage.clickContinueSetup();
    });

    await test.step("Create authentication - validate and set password", async () => {
      await welcomePage.validateAuthenticationUrl();
      await welcomePage.validateTitle("Create an admin login for your miner");
      await welcomePage.validateUsernameFieldDisabledWithValue("admin");
      await welcomePage.inputPassword(testConfig.admin.password);
      await welcomePage.inputConfirmPassword(testConfig.admin.password);
      await welcomePage.clickContinue();
    });

    await test.step("Mining pool setup - validate page and warning", async () => {
      await welcomePage.validateMiningPoolUrl();
      await welcomePage.validateTitle("Pools");
      await welcomePage.validateTextIsVisible("Add up to 3 pools for your miner.");
      await welcomePage.clickButton("Continue");
      await welcomePage.validateDefaultPoolWarningVisible();
      await welcomePage.validateDefaultPoolWarningText();
      await welcomePage.closeDefaultPoolWarning();
    });

    await test.step("Add default mining pool", async () => {
      await poolsPage.clickAddPool();
      await poolsPage.validatePoolModalOpened();
      await poolsPage.inputPoolName(poolName, 0);
      await poolsPage.inputPoolUrl(testConfig.pool.url, 0);
      await poolsPage.inputPoolUsername(poolUsername, 0);
      await poolsPage.inputPoolPassword(poolPassword, 0);
      await poolsPage.clickTestConnection();
      await poolsPage.validateConnectionSuccessful();
      await poolsPage.clickSave();
      await poolsPage.validateModalIsClosed();
    });

    await test.step("Submit one pool", async () => {
      await welcomePage.clickButton("Continue");
    });

    await test.step("Confirm continue without backup pool", async () => {
      await welcomePage.validateTitle("Continue without a backup pool?");
      await welcomePage.validateButtonIsVisible("Add a backup pool");
      await welcomePage.clickButton("Continue without backup");
      await welcomePage.confirmAirCoolingIfPrompted();
    });

    await test.step("Your miner is ready", async () => {
      await welcomePage.validateTitle("Your miner is ready");
      await welcomePage.validateTextIsVisible("Testing your mining pool connections");
      await welcomePage.clickButton("Continue");
    });

    await test.step("Validate user is logged in to dashboard", async () => {
      await homePage.validateLoggedIn();
    });
  });
});
