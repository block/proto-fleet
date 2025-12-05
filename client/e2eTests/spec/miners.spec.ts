/* eslint-disable playwright/expect-expect */
import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";

test.describe.serial("Miners", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  // Figure out how/when to do this, to avoid impact on other tests
  // test("Put miners to sleep", async ({ authPage, minersPage }) => {
  //   await test.step("Login as admin", async () => {
  //     await authPage.inputUsername(testConfig.users.admin.username);
  //     await authPage.inputPassword(testConfig.users.admin.password);
  //     await authPage.clickLogin();
  //     await authPage.validateLoggedIn();
  //   });

  //   await test.step("Navigate to miners page", async () => {
  //     await authPage.navigateToMinersPage();
  //     await minersPage.waitForMinersTitle();
  //     await minersPage.validateAmountOfMiners(testConfig.expectedMinerCount);
  //   });

  //   await test.step("Select all miners and shut them down", async () => {
  //     await minersPage.clickSelectAllCheckbox();
  //     await minersPage.clickActionsMenuButton();
  //     await minersPage.clickShutdownButton();
  //     await minersPage.clickShutdownConfirm();
  //   });

  //   await test.step("Validate update process", async () => {
  //     await minersPage.validateUpdateInProgress();
  //     await minersPage.validateUpdateCompleted();
  //   });

  //   await test.step("Validate all miners are sleeping", async () => {
  //     await minersPage.validateAllMinersStatus("Sleeping");
  //   });
  // });

  test("Wake miners up", async ({ authPage, minersPage }) => {
    await test.step("Login as admin", async () => {
      await authPage.inputUsername(testConfig.users.admin.username);
      await authPage.inputPassword(testConfig.users.admin.password);
      await authPage.clickLogin();
      await authPage.validateLoggedIn();
    });

    await test.step("Navigate to miners page", async () => {
      await authPage.navigateToMinersPage();
      await minersPage.waitForMinersTitle();
      await minersPage.validateAmountOfMiners(testConfig.expectedMinerCount);
    });

    await test.step("Select all miners and wake them up", async () => {
      await minersPage.clickSelectAllCheckbox();
      await minersPage.clickActionsMenuButton();
      await minersPage.clickWakeUpButton();
      await minersPage.clickWakeUpConfirm();
    });

    await test.step("Validate update process", async () => {
      await minersPage.validateUpdateInProgress();
      await minersPage.validateUpdateCompleted();
    });

    await test.step("Validate all miners are hashing", async () => {
      await minersPage.clickSelectAllCheckbox();
      await minersPage.validateAllMinersStatus("Hashing");
    });
  });
});
