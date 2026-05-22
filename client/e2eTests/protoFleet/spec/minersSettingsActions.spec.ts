import { test } from "../fixtures/pageFixtures";
import { generateRandomText } from "../helpers/testDataHelper";

type WorkerNameRestoreTarget = {
  ipAddress: string;
  workerName: string;
};

test.describe("Miner Settings Actions", () => {
  let workerNameRestoreTargets: WorkerNameRestoreTarget[] = [];

  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test.afterEach(async ({ commonSteps, minersPage, loginModal }) => {
    if (workerNameRestoreTargets.length > 0) {
      await commonSteps.goToMinersPage();
      await minersPage.filterRigMiners();

      for (const restoreTarget of workerNameRestoreTargets) {
        await minersPage.clickMinerThreeDotsButton(restoreTarget.ipAddress);
        await minersPage.clickUpdateWorkerNameButton();
        await loginModal.loginAsAdminForWorkerNames();
        await minersPage.validateUpdateWorkerNameModalOpened();
        await minersPage.fillUpdateWorkerNameInput(restoreTarget.workerName);
        await minersPage.clickSaveInModal();
        await minersPage.continueUpdateWorkerNameNoChangesIfVisible();
        await minersPage.validateMinerWorkerName(restoreTarget.ipAddress, restoreTarget.workerName);
      }

      workerNameRestoreTargets = [];
    }
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

  test("Update worker name from a miner action menu and restore the original value", async ({
    minersPage,
    commonSteps,
    loginModal,
  }) => {
    let minerIp: string;
    let originalWorkerName: string;
    const updatedWorkerName = generateRandomText("worker-e2e");

    await test.step("Find a Proto rig with an existing worker name", async () => {
      await commonSteps.loginAsAdmin();
      await commonSteps.goToMinersPage();
      await minersPage.filterRigMiners();
      const [selectedWorkerNamedMiner] = await minersPage.getAuthenticatedMinersWithNonEmptyWorkerNames(1);
      minerIp = selectedWorkerNamedMiner.ipAddress;
      originalWorkerName = selectedWorkerNamedMiner.workerName;
    });

    await test.step("Update the worker name through the single-miner action flow", async () => {
      workerNameRestoreTargets = [{ ipAddress: minerIp, workerName: originalWorkerName }];

      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.clickUpdateWorkerNameButton();
      await loginModal.loginAsAdminForWorkerNames();
      await minersPage.validateUpdateWorkerNameModalOpened();
      await minersPage.fillUpdateWorkerNameInput(updatedWorkerName);
      await minersPage.clickSaveInModal();

      await minersPage.validateTextInToastGroup("Worker name updated");
      await minersPage.validateMinerWorkerName(minerIp, updatedWorkerName);
    });
  });

  test("Bulk update worker names action updates the selected miners", async ({
    minersPage,
    commonSteps,
    loginModal,
    page,
  }) => {
    let selectedMiners: WorkerNameRestoreTarget[] = [];
    const updatedWorkerNamePrefix = generateRandomText("worker-bulk");

    await test.step("Select a Proto rig from the miners table", async () => {
      await commonSteps.loginAsAdmin();
      await commonSteps.goToMinersPage();
      await minersPage.filterRigMiners();

      selectedMiners = await minersPage.getAuthenticatedMinersWithNonEmptyWorkerNames(2);
      workerNameRestoreTargets = selectedMiners;

      await minersPage.clickMinerCheckbox(selectedMiners[0].ipAddress);
      await minersPage.clickMinerCheckbox(selectedMiners[1].ipAddress);
      await minersPage.validateActionBarMinerCount(2);
    });

    await test.step("Authenticate into the bulk worker-name flow and apply the updates", async () => {
      const requestPromise = page.waitForRequest(/UpdateWorkerNames/);
      const responsePromise = page.waitForResponse(/UpdateWorkerNames/);

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
      await minersPage.clickBulkWorkerNameSave();
      await minersPage.continueBulkRenameOverwriteWarningIfVisible();

      const request = await requestPromise;
      const response = await responsePromise;
      const requestBody = request.postDataJSON();

      test.expect(request.method()).toBe("POST");
      test.expect(requestBody).toHaveProperty("deviceSelector");
      test.expect(requestBody.deviceSelector).toHaveProperty("includeDevices");
      test.expect(requestBody.deviceSelector.includeDevices.deviceIdentifiers).toHaveLength(2);
      test.expect(JSON.stringify(requestBody)).toContain(updatedWorkerNamePrefix);
      test.expect(response.status()).toBe(200);
    });
  });

  test("Manage security opens from the miner action menu and validates password input", async ({
    minersPage,
    commonSteps,
    loginModal,
    page,
  }) => {
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
      await minersPage.validateTitle("Miners");
    });
  });
});
