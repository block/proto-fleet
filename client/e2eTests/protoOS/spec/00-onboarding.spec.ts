/* eslint-disable playwright/expect-expect */
import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";
import { generateRandomText } from "../helpers/testDataHelper";

test.describe("Onboarding", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test("Complete onboarding flow @setup", async ({ authPage, homePage, poolPage }) => {
    const poolName = generateRandomText("PoolName");

    await test.step("Welcome screen - validate and start setup", async () => {
      await authPage.validateWelcomeUrl();
      await authPage.validateTextIsVisible("Miner setup");
      await authPage.clickGetStartedButton();
    });

    await test.step("Verify miner - validate information and continue", async () => {
      await authPage.validateVerifyUrl();
      await authPage.validateTitle("Is this the miner you want to set up?");
      await authPage.validateTextIsVisible("Controller Serial");
      await authPage.validateTextIsVisible("Mac Address");
      await authPage.clickContinueSetup();
    });

    await test.step("Create authentication - validate and set password", async () => {
      await authPage.validateAuthenticationUrl();
      await authPage.validateTitle("Create an admin login for your miner");
      await authPage.validateUsernameFieldDisabledWithValue("admin");
      await authPage.inputPassword(testConfig.users.password);
      await authPage.inputConfirmPassword(testConfig.users.password);
      await authPage.clickContinue();
    });

    await test.step("Mining pool setup - validate page and warning", async () => {
      await authPage.validateMiningPoolUrl();
      await authPage.validateTitle("Pools");
      await authPage.validateTextIsVisible("Add up to 3 pools for your miner.");
      await authPage.click("Continue");
      await authPage.validateDefaultPoolWarningVisible();
      await authPage.validateDefaultPoolWarningText();
      await authPage.closeDefaultPoolWarning();
    });

    await test.step("Add default mining pool", async () => {
      await poolPage.clickAddPool();
      await poolPage.validatePoolModalOpened();
      await poolPage.inputPoolName(poolName, 0);
      await poolPage.inputPoolUrl(testConfig.pool.url, 0);
      await poolPage.inputPoolUsername(testConfig.pool.username, 0);
      await poolPage.inputPoolPassword(testConfig.pool.password, 0);
      await poolPage.clickTestConnection();
      await poolPage.validateConnectionSuccessful();
      await poolPage.clickSave();
      await poolPage.validateModalIsClosed();
    });

    // await test.step("Add first backup pool", async () => {
    //   await poolPage.clickAddPool();
    //   await poolPage.validatePoolModalOpened();
    //   await poolPage.inputPoolUrl(testConfig.pool.url, 1);
    //   await poolPage.inputPoolUsername(testConfig.pool.username, 1);
    //   await poolPage.inputPoolPassword(testConfig.pool.password, 1);
    //   await poolPage.clickTestConnection();
    //   await poolPage.validateConnectionSuccessful();
    //   await poolPage.clickSave();
    //   await poolPage.validateModalIsClosed();
    // });

    // await test.step("Add second backup pool", async () => {
    //   await poolPage.clickAddPool();
    //   await poolPage.validatePoolModalOpened();
    //   await poolPage.inputPoolUrl(testConfig.pool.url, 2);
    //   await poolPage.inputPoolUsername(testConfig.pool.username, 2);
    //   await poolPage.inputPoolPassword(testConfig.pool.password, 2);
    //   await poolPage.clickTestConnection();
    //   await poolPage.validateConnectionSuccessful();
    //   await poolPage.clickSave();
    //   await poolPage.validateModalIsClosed();
    // });

    await test.step("Submit one pool", async () => {
      await authPage.click("Continue");
    });

    await test.step("Confirm continue without backup pool", async () => {
      await authPage.validateTitle("Continue without a backup pool?");
      await authPage.validateButtonIsVisible("Add a backup pool");
      await authPage.click("Continue without backup");
    });

    await test.step("Your miner is ready", async () => {
      await authPage.validateTitle("Configuring your miner");
      await authPage.validateTitle("Your miner is ready");
      await authPage.validateTextIsVisible("Testing your mining pool connections");
      await authPage.click("Continue");
    });

    await test.step("Validate user is logged in to dashboard", async () => {
      await homePage.validateLoggedIn();
    });
  });
});
