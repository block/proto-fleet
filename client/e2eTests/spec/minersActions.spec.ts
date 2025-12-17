/* eslint-disable playwright/expect-expect */
import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";

test.describe.serial("Miners", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test("Put miners to SLEEP", async ({ authPage, minersPage }) => {
    await test.step("Login as admin", async () => {
      await authPage.inputUsername(testConfig.users.admin.username);
      await authPage.inputPassword(testConfig.users.admin.password);
      await authPage.clickLogin();
      await authPage.validateLoggedIn();
    });

    await test.step("Navigate to miners page", async () => {
      await authPage.navigateToMinersPage();
      await minersPage.waitForMinersTitle();
      // Workaround: antminers always have status 'hashing'
      await minersPage.waitForMinersListToLoad();
      await minersPage.filterProtoMiners();
    });

    let minerIp: string;
    await test.step("Select first miner and shut it down", async () => {
      minerIp = await minersPage.getMinerIpAddressByIndex(0);
      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.clickShutdownButton();
      await minersPage.clickShutdownConfirm();
    });

    await test.step("Validate update process", async () => {
      await minersPage.validateUpdateInProgress();
      await minersPage.validateUpdateCompleted();
    });

    await test.step("Validate miner is sleeping", async () => {
      await minersPage.validateMinerStatus(minerIp, "Sleeping");
    });
  });

  test("WAKE miners up", async ({ authPage, minersPage }) => {
    await test.step("Login as admin", async () => {
      await authPage.inputUsername(testConfig.users.admin.username);
      await authPage.inputPassword(testConfig.users.admin.password);
      await authPage.clickLogin();
      await authPage.validateLoggedIn();
    });

    await test.step("Navigate to miners page", async () => {
      await authPage.navigateToMinersPage();
      await minersPage.waitForMinersTitle();
      await minersPage.waitForMinersListToLoad();
    });

    await test.step("Select first miner and wake it up", async () => {
      await minersPage.clickSelectAllCheckbox();
      await minersPage.clickActionsMenuButton();
      await minersPage.clickWakeUpButton();
      await minersPage.clickWakeUpConfirm();
    });

    await test.step("Validate update process", async () => {
      await minersPage.validateUpdateInProgress();
      await minersPage.validateUpdateCompleted();
    });

    await test.step("Validate miner is hashing", async () => {
      await minersPage.validateAllMinersStatus("Hashing");
    });
  });

  test("UNPAIR a single miner", async ({ authPage, minersPage }) => {
    await test.step("Login as admin", async () => {
      await authPage.inputUsername(testConfig.users.admin.username);
      await authPage.inputPassword(testConfig.users.admin.password);
      await authPage.clickLogin();
      await authPage.validateLoggedIn();
    });

    await test.step("Navigate to miners page", async () => {
      await authPage.navigateToMinersPage();
      await minersPage.waitForMinersTitle();
      await minersPage.waitForMinersListToLoad();
    });

    let minerCount: number;
    let minerIp: string;
    await test.step("Select a miner and unpair it", async () => {
      minerCount = await minersPage.getMinersCount();
      minerIp = await minersPage.getMinerIpAddressByIndex(0);
      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.clickUnpairButton();
      await minersPage.clickUnpairConfirm();
    });

    await test.step("Validate update process", async () => {
      await minersPage.validateUpdateInProgress();
      await minersPage.validateUpdateCompleted();
    });

    await test.step("Validate miner was unpaired", async () => {
      await minersPage.validateMinerNotPresent(minerIp);
      await minersPage.validateAmountOfMiners(minerCount - 1);
    });
  });

  test("UNPAIR multiple miners", async ({ authPage, minersPage }) => {
    await test.step("Login as admin", async () => {
      await authPage.inputUsername(testConfig.users.admin.username);
      await authPage.inputPassword(testConfig.users.admin.password);
      await authPage.clickLogin();
      await authPage.validateLoggedIn();
    });

    await test.step("Navigate to miners page", async () => {
      await authPage.navigateToMinersPage();
      await minersPage.waitForMinersTitle();
      await minersPage.waitForMinersListToLoad();
    });

    let minerCount: number;
    let minerIp1: string;
    let minerIp2: string;
    let minerIp3: string;
    await test.step("Select multiple miners and unpair them", async () => {
      minerCount = await minersPage.getMinersCount();
      minerIp1 = await minersPage.getMinerIpAddressByIndex(0);
      minerIp2 = await minersPage.getMinerIpAddressByIndex(1);
      minerIp3 = await minersPage.getMinerIpAddressByIndex(2);
      await minersPage.clickMinerCheckbox(minerIp1);
      await minersPage.validateActionBarMinerCount(1);
      await minersPage.clickMinerCheckbox(minerIp2);
      await minersPage.validateActionBarMinerCount(2);
      await minersPage.clickMinerCheckbox(minerIp3);
      await minersPage.validateActionBarMinerCount(3);
      await minersPage.clickActionsMenuButton();
      await minersPage.clickUnpairButton();
      await minersPage.clickUnpairConfirm();
    });

    await test.step("Validate update process", async () => {
      await minersPage.validateUpdateInProgress();
      await minersPage.validateUpdateCompleted();
    });

    await test.step("Validate miners were unpaired", async () => {
      await minersPage.validateMinerNotPresent(minerIp1);
      await minersPage.validateMinerNotPresent(minerIp2);
      await minersPage.validateMinerNotPresent(minerIp3);
      await minersPage.validateAmountOfMiners(minerCount - 3);
    });
  });

  test("ADD a single miner", async ({ authPage, minersPage, addMinersPage }) => {
    await test.step("Login as admin", async () => {
      await authPage.inputUsername(testConfig.users.admin.username);
      await authPage.inputPassword(testConfig.users.admin.password);
      await authPage.clickLogin();
      await authPage.validateLoggedIn();
    });

    await test.step("Navigate to miners page", async () => {
      await authPage.navigateToMinersPage();
      await minersPage.waitForMinersTitle();
      await minersPage.waitForMinersListToLoad();
    });

    let minerIp: string;
    let minerCount: number;
    await test.step("Add a single miner", async () => {
      minerCount = await minersPage.getMinersCount();
      await minersPage.clickAddMinersButton();
      await addMinersPage.clickFindMiners();
      await addMinersPage.clickChooseMiners();
      await addMinersPage.clickSelectNone();
      minerIp = await addMinersPage.getMinerIpAddressByIndex(0);
      await addMinersPage.clickMinerCheckbox(minerIp);
      await addMinersPage.clickDone();
      await addMinersPage.clickContinueWithXMiners(1);
    });

    await test.step("Validate miner was added", async () => {
      await minersPage.waitForMinersTitle();
      await minersPage.waitForMinersListToLoad();
      await minersPage.validateMinerInList(minerIp);
      await minersPage.validateAmountOfMiners(minerCount + 1);
    });
  });

  test("ADD multiple miners", async ({ authPage, minersPage, addMinersPage }) => {
    await test.step("Login as admin", async () => {
      await authPage.inputUsername(testConfig.users.admin.username);
      await authPage.inputPassword(testConfig.users.admin.password);
      await authPage.clickLogin();
      await authPage.validateLoggedIn();
    });

    await test.step("Navigate to miners page", async () => {
      await authPage.navigateToMinersPage();
      await minersPage.waitForMinersTitle();
      await minersPage.waitForMinersListToLoad();
    });

    let minerIp1: string;
    let minerIp2: string;
    let minerCount: number;
    await test.step("Add multiple miners", async () => {
      minerCount = await minersPage.getMinersCount();
      await minersPage.clickAddMinersButton();
      await addMinersPage.clickFindMiners();
      await addMinersPage.clickChooseMiners();
      await addMinersPage.clickSelectNone();
      minerIp1 = await addMinersPage.getMinerIpAddressByIndex(0);
      minerIp2 = await addMinersPage.getMinerIpAddressByIndex(1);
      await addMinersPage.clickMinerCheckbox(minerIp1);
      await addMinersPage.clickMinerCheckbox(minerIp2);
      await addMinersPage.clickDone();
      await addMinersPage.clickContinueWithXMiners(2);
    });

    await test.step("Validate miners were added", async () => {
      await minersPage.waitForMinersTitle();
      await minersPage.waitForMinersListToLoad();
      await minersPage.validateMinerInList(minerIp1);
      await minersPage.validateMinerInList(minerIp2);
      await minersPage.validateAmountOfMiners(minerCount + 2);
    });
  });

  test("CLEANUP: Re-authenticate added miners", async ({ authPage, homePage, minersPage }) => {
    // Workaround - re-added Antminers need authentication again
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
