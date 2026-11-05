import { test } from "../fixtures/pageFixtures";
import { generateRandomText } from "../helpers/testDataHelper";

const primaryUrl = "stratum+tcp://mine.ocean.xyz:3334";
const backupUrl = "stratum+tcp://backup.example:3333";

test.describe("Pool configurations", () => {
  test.beforeEach(async ({ page, commonSteps }) => {
    await page.goto("/");
    await commonSteps.loginAsAdmin();
  });

  test("Create, edit, and reorder a saved pool configuration", async ({
    settingsPage,
    settingsPoolsPage,
    newPoolModal,
  }) => {
    const name = generateRandomText("PoolConfiguration");
    const username = generateRandomText("payout");
    await settingsPage.navigateToMiningPoolsSettings();
    await settingsPoolsPage.validateMiningPoolsPageOpened();
    try {
      await test.step("Create a complete primary configuration", async () => {
        await settingsPoolsPage.clickAddPool();
        await newPoolModal.validatePoolModalOpened();
        await newPoolModal.validateConnectionDisabled();
        await newPoolModal.inputPoolName(name);
        await newPoolModal.inputPoolUrl(primaryUrl);
        await newPoolModal.inputPoolUsername(username);
        await newPoolModal.clickSaveNewPool();
        await settingsPoolsPage.validatePoolEntryByUniqueName(name, primaryUrl, username);
      });

      await test.step("Save a backup as the new primary", async () => {
        await settingsPoolsPage.editConfiguration(name);
        await newPoolModal.addBackup(backupUrl, username);
        await newPoolModal.moveBackupToPrimary();
        await newPoolModal.clickSaveNewPool();
        await settingsPoolsPage.validatePoolEntryByUniqueName(name, backupUrl, username);
      });
    } finally {
      await settingsPoolsPage.deletePoolByNameIfVisible(name);
    }
  });

  test("Assign a reviewed default configuration to all miners @setup", async ({
    commonSteps,
    minersPage,
    loginModal,
    editPoolPage,
    newPoolModal,
  }) => {
    const name = generateRandomText("DefaultPoolConfiguration");
    await commonSteps.goToMinersPage();
    await minersPage.clickSelectAllCheckbox();
    const count = await minersPage.getSelectedMinersCount();
    await minersPage.clickActionsMenuButton();
    await minersPage.clickEditMiningPoolButton();
    await loginModal.loginAsAdmin();
    await editPoolPage.createConfiguration();
    await newPoolModal.inputPoolName(name);
    await newPoolModal.inputPoolUrl(primaryUrl);
    await newPoolModal.inputPoolUsername(generateRandomText("payout"));
    await newPoolModal.clickSaveNewPool();
    await editPoolPage.validateSelectedConfiguration(name);
    await editPoolPage.reviewAssignment(count);
    await editPoolPage.confirmAssignment();
    await minersPage.validateNoActionableMinerWithIssue("Pool required", count);
  });
});
