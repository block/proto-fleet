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
      const firstAuthenticatedMinerIp = await minersPage.getAuthenticatedMinerIpAddressByIndex(0);
      const secondAuthenticatedMinerIp = await minersPage.getAuthenticatedMinerIpAddressByIndex(1);
      const resolvedCandidates = await Promise.all(
        [firstAuthenticatedMinerIp, secondAuthenticatedMinerIp].map(async (ipAddress) => ({
          ipAddress,
          workerName: await minersPage.getMinerWorkerName(ipAddress),
        })),
      );
      const namedCandidate = resolvedCandidates.find(({ workerName }) => workerName && workerName !== "—");
      test.expect(namedCandidate).toBeDefined();

      const selectedWorkerNamedMiner = namedCandidate!;
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

    await test.step("Select a Proto rig from the miners table", async () => {
      await commonSteps.loginAsAdmin();
      await commonSteps.goToMinersPage();
      await minersPage.filterRigMiners();

      const minerIp1 = await minersPage.getAuthenticatedMinerIpAddressByIndex(0);
      const minerIp2 = await minersPage.getAuthenticatedMinerIpAddressByIndex(1);

      selectedMiners = [
        { ipAddress: minerIp1, workerName: await minersPage.getMinerWorkerName(minerIp1) },
        { ipAddress: minerIp2, workerName: await minersPage.getMinerWorkerName(minerIp2) },
      ];
      workerNameRestoreTargets = selectedMiners;

      await minersPage.clickMinerCheckbox(minerIp1);
      await minersPage.clickMinerCheckbox(minerIp2);
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
      await minersPage.clickBulkRenamePropertyToggle("fixed-serial-number");
      await minersPage.clickBulkWorkerNameSave();
      await minersPage.continueBulkRenameOverwriteWarningIfVisible();

      const request = await requestPromise;
      const response = await responsePromise;
      const requestBody = request.postDataJSON();

      test.expect(request.method()).toBe("POST");
      test.expect(requestBody).toHaveProperty("deviceSelector");
      test.expect(requestBody.deviceSelector).toHaveProperty("includeDevices");
      test.expect(requestBody.deviceSelector.includeDevices.deviceIdentifiers).toHaveLength(2);
      test.expect(response.status()).toBe(200);

      await minersPage.validateTextInToastGroup("Updated 2 miners");

      for (const selectedMiner of selectedMiners) {
        const updatedWorkerName = await minersPage.getMinerWorkerName(selectedMiner.ipAddress);
        test.expect(updatedWorkerName).not.toBe("");
        test.expect(updatedWorkerName).not.toBe("—");
        test.expect(updatedWorkerName).not.toBe(selectedMiner.workerName);
      }
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

    await test.step("Open the password modal and validate that current password is required", async () => {
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
