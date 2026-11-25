import { test } from "../fixtures/pageFixtures";
import { generateRandomText } from "../helpers/testDataHelper";

const primaryUrl = "stratum+tcp://mine.ocean.xyz:3334";

test.describe("Pool configurations", () => {
  test.beforeEach(async ({ page, commonSteps }) => {
    await page.goto("/");
    await commonSteps.loginAsAdmin();
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
