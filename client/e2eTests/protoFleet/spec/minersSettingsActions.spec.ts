import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";
import { submitWithPasswordAttemptRetry } from "../helpers/passwordAttemptRetry";
import { generateRandomText } from "../helpers/testDataHelper";

test.describe("Miner Settings Actions", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test("Download logs from a miner action menu starts a log bundle download", async ({
    minersPage,
    page,
    commonSteps,
  }) => {
    let minerIp: string;

    await test.step("Open the miners page and focus on Proto rigs", async () => {
      await commonSteps.loginAsAdmin();
      await commonSteps.goToMinersPage();
      await minersPage.filterRigMiners();
      minerIp = await minersPage.getAuthenticatedMinerIpAddressByIndex(0);
    });

    await test.step("Download logs from the single-miner actions menu", async () => {
      const downloadPromise = page.waitForEvent("download");

      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.clickDownloadLogsButton();

      const download = await downloadPromise;
      test.expect(download.suggestedFilename()).toMatch(/\.(zip|csv)$/i);
      await minersPage.validateTextInToastGroup("Downloaded logs");
    });
  });

  if (testConfig.target !== "real") {
    test("Single worker-name update rejects a Proto rig without stored pool readback @smoke", async ({
      minersPage,
      commonSteps,
      loginModal,
      page,
    }) => {
      await commonSteps.loginAsAdmin();
      await commonSteps.goToMinersPage();
      await minersPage.filterRigMiners();
      const [miner] = await minersPage.getAuthenticatedMinersWithNonEmptyWorkerNames(1);
      const updatedWorkerName = generateRandomText("worker-rejected");

      await minersPage.clickMinerThreeDotsButton(miner.ipAddress);
      await minersPage.clickUpdateWorkerNameButton();
      await loginModal.loginAsAdminForWorkerNames();
      await minersPage.validateUpdateWorkerNameModalOpened();
      await minersPage.fillUpdateWorkerNameInput(updatedWorkerName);
      const response = await submitWithPasswordAttemptRetry(page, /\/UpdateWorkerNames$/, async () => {
        await minersPage.clickSaveInModal();
      });
      test.expect(response.status()).toBe(400);
      test.expect(await response.json()).toMatchObject({
        code: "failed_precondition",
        message: test.expect.stringContaining("complete stored pool readback is unsupported"),
      });
      await minersPage.validateTextInToastGroup("Failed to update worker name");
      await page.reload();
      await minersPage.validateMinersPageOpened();
      await test.expect.poll(() => minersPage.getMinerWorkerName(miner.ipAddress)).toBe(miner.workerName);
    });

    test("Bulk worker-name update rejects Proto rigs without changing their names", async ({
      minersPage,
      commonSteps,
      loginModal,
      page,
    }) => {
      await commonSteps.loginAsAdmin();
      await commonSteps.goToMinersPage();
      await minersPage.filterRigMiners();
      const selectedMiners = await minersPage.getAuthenticatedMinersWithNonEmptyWorkerNames(2);
      const updatedWorkerNamePrefix = generateRandomText("worker-bulk-rejected");
      for (const miner of selectedMiners) {
        await minersPage.clickMinerCheckbox(miner.ipAddress);
      }
      await minersPage.validateActionBarMinerCount(2);

      await minersPage.clickActionsMenuButton();
      await minersPage.clickUpdateWorkerNameButton();
      await loginModal.loginAsAdminForWorkerNames();
      await minersPage.validateBulkWorkerNameModalOpened();
      await minersPage.validateBulkWorkerNameSaveLabel("Apply to 2 miners");
      await minersPage.clickBulkRenamePropertyToggle("custom");
      await minersPage.clickBulkRenamePropertyOptions("custom");
      await minersPage.fillCustomPropertyPrefix(updatedWorkerNamePrefix);
      await minersPage.saveCustomPropertyOptions();
      await minersPage.validateModalIsClosed();
      const response = await submitWithPasswordAttemptRetry(page, /\/UpdateWorkerNames$/, async () => {
        await minersPage.clickBulkWorkerNameSave();
        await minersPage.continueBulkRenameOverwriteWarningIfVisible();
      });
      const request = response.request();
      test.expect(request.method()).toBe("POST");
      test.expect(request.postDataJSON().deviceSelector.includeDevices.deviceIdentifiers).toHaveLength(2);
      test.expect(JSON.stringify(request.postDataJSON().nameConfig)).toContain(updatedWorkerNamePrefix);
      test.expect(response.status()).toBe(400);
      test.expect(await response.json()).toMatchObject({
        code: "failed_precondition",
        message: test.expect.stringContaining("complete stored pool readback is unsupported"),
      });
      await minersPage.validateTextInToastGroup("Failed to update worker names");
      await page.reload();
      await minersPage.validateMinersPageOpened();
      for (const miner of selectedMiners) {
        await test.expect.poll(() => minersPage.getMinerWorkerName(miner.ipAddress)).toBe(miner.workerName);
      }
    });
  }

  test(
    "Manage security opens from the miner action menu and validates password input",
    { tag: "@smoke" },
    async ({ minersPage, commonSteps, loginModal, page }) => {
      await test.step("Open Manage security for a Proto rig", async () => {
        await commonSteps.loginAsAdmin();
        await commonSteps.goToMinersPage();
        await minersPage.filterRigMiners();

        const minerIp = await minersPage.getAuthenticatedMinerIpAddressByIndex(0);
        await minersPage.clickMinerThreeDotsButton(minerIp);
        await minersPage.clickManageSecurityButton();
        await loginModal.loginAsAdminForSecurity();
        await minersPage.validateManageSecurityModalOpened();
      });

      await test.step("Open the password modal and validate the password mismatch state", async () => {
        await minersPage.clickManageSecurityUpdateButton();
        await minersPage.validateTitleInModal("Update the admin login for your miners");
        await minersPage.inputCurrentMinerPassword("root");
        await minersPage.inputNewMinerPassword("ProtoRigPass123!");
        await minersPage.inputConfirmMinerPassword("ProtoRigPass1234!");
        await minersPage.clickIn("Continue", "modal");
        await minersPage.validateTextInModal("Passwords don't match");

        await page.getByTestId("modal").getByTestId("header-icon-button").click();
        await minersPage.closeManageSecurityModal();
        await minersPage.validateMinersPageOpened();
      });
    },
  );
});
