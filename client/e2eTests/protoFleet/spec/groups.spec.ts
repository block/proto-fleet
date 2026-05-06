import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";
import { CommonSteps } from "../helpers/commonSteps";
import { PROTO_RIG_MODEL } from "../helpers/minerModels";
import { generateRandomText } from "../helpers/testDataHelper";
import { AuthPage } from "../pages/auth";
import { GroupsPage } from "../pages/groups";
import { MinersPage } from "../pages/miners";

test.describe("Groups", () => {
  test.beforeEach(async ({ page, groupsPage, commonSteps }) => {
    await page.goto("/");
    await commonSteps.loginAsAdmin();
    await groupsPage.navigateToGroupsPage();
    await cleanupAllGroups(groupsPage);
  });

  test.afterAll("CLEANUP: Delete all groups", async ({ browser }, testInfo) => {
    const isMobile = testInfo.project.use?.isMobile ?? false;
    const context = await browser.newContext({ baseURL: testConfig.baseUrl });
    try {
      const page = await context.newPage();
      await page.goto("/");

      const authPage = new AuthPage(page, isMobile);
      const minersPage = new MinersPage(page, isMobile);
      const groupsPage = new GroupsPage(page, isMobile);
      const commonSteps = new CommonSteps(authPage, minersPage);

      await commonSteps.loginAsAdmin();
      await groupsPage.navigateToGroupsPage();
      await cleanupAllGroups(groupsPage);
    } finally {
      await context.close();
    }
  });

  async function cleanupAllGroups(groupsPage: GroupsPage) {
    const existingGroupNames = await groupsPage.listSavedGroupNames();

    if (existingGroupNames.length === 0) {
      return;
    }

    const automationGroups = existingGroupNames.filter((groupName) => groupName.startsWith("automation"));

    for (const groupName of automationGroups) {
      await groupsPage.openSavedGroup(groupName);
      await groupsPage.clickDeleteGroupInModal();
      await groupsPage.clickDeleteConfirm();
      await groupsPage.validateSavedGroupNotVisible(groupName);
    }
  }

  test("Create, edit, and delete groups", async ({ groupsPage }) => {
    const groupName = generateRandomText("automation");
    const editedGroupName = generateRandomText("automation-edited");

    await test.step("Create new group with all miners", async () => {
      await groupsPage.clickAddGroupButton();
      await groupsPage.inputGroupName(groupName);

      await groupsPage.waitForModalListToLoad();
      const allMinersCount = await groupsPage.getModalListRowCount();

      await groupsPage.clickSelectAllCheckboxInModal();
      await groupsPage.clickSaveInModal();

      await groupsPage.validateTextInToast(`Group "${groupName}" created`);
      await groupsPage.validateSavedGroupVisible(groupName);
      await groupsPage.validateSavedGroupMinerCount(groupName, allMinersCount);
    });

    await test.step("Edit group to only rig miners", async () => {
      await groupsPage.openSavedGroup(groupName);
      await groupsPage.waitForModalListToLoad();

      await groupsPage.inputGroupName(editedGroupName);

      // clear previous selection
      await groupsPage.clickSelectAllCheckboxInModal();

      await groupsPage.filterModalType(PROTO_RIG_MODEL);
      await groupsPage.waitForModalListToLoad();

      await groupsPage.clickSelectAllCheckboxInModal();
      const rigMinersCount = await groupsPage.getModalListRowCount();

      await groupsPage.clickSaveInModal();

      await groupsPage.validateTextInToast(`Group "${editedGroupName}" updated`);
      await groupsPage.validateSavedGroupVisible(editedGroupName);
      await groupsPage.validateSavedGroupMinerCount(editedGroupName, rigMinersCount);
    });

    await test.step("Delete group", async () => {
      await groupsPage.openSavedGroup(editedGroupName);
      await groupsPage.clickDeleteGroupInModal();
      await groupsPage.validateTitle(`Delete "${editedGroupName}"?`);
      await groupsPage.clickDeleteConfirm();

      await groupsPage.validateTextInToast(`Group "${editedGroupName}" deleted`);
      await groupsPage.validateSavedGroupNotVisible(editedGroupName);
    });
  });

  test("Validate groups association to miners", async ({ groupsPage }) => {
    const group1Name = generateRandomText("automation1");
    const group2Name = generateRandomText("automation2");
    const group3Name = generateRandomText("automation3");
    const minerIps: string[] = [];

    await test.step("Capture 5 clean miners with no existing groups", async () => {
      await groupsPage.clickAddGroupButton();
      await groupsPage.waitForModalListToLoad();
      minerIps.push(...(await groupsPage.getUngroupedMinerIps(5)));
      test.expect(minerIps).toHaveLength(5);
      await groupsPage.closeModal();
    });

    await test.step("Create group1 with miners 0-2", async () => {
      await groupsPage.clickAddGroupButton();
      await groupsPage.inputGroupName(group1Name);
      await groupsPage.waitForModalListToLoad();
      for (const ip of minerIps.slice(0, 3)) {
        await groupsPage.selectMinerByIp(ip);
      }
      await groupsPage.clickSaveInModal();
      await groupsPage.validateTextInToast(`Group "${group1Name}" created`);
      await groupsPage.validateSavedGroupVisible(group1Name);
      await groupsPage.validateSavedGroupMinerCount(group1Name, 3);
    });

    await test.step("Validate specific miners have group1 in group column", async () => {
      await groupsPage.clickAddGroupButton();
      await groupsPage.waitForModalListToLoad();
      await groupsPage.validateMinerGroupsByIp(minerIps[0], group1Name);
      await groupsPage.validateMinerGroupsByIp(minerIps[1], group1Name);
      await groupsPage.validateMinerGroupsByIp(minerIps[2], group1Name);
      await groupsPage.closeModal();
    });

    await test.step("Create group2 with miners 1-3", async () => {
      await groupsPage.clickAddGroupButton();
      await groupsPage.inputGroupName(group2Name);
      await groupsPage.waitForModalListToLoad();
      for (const ip of minerIps.slice(1, 4)) {
        await groupsPage.selectMinerByIp(ip);
      }
      await groupsPage.clickSaveInModal();
      await groupsPage.validateTextInToast(`Group "${group2Name}" created`);
      await groupsPage.validateSavedGroupVisible(group2Name);
      await groupsPage.validateSavedGroupMinerCount(group2Name, 3);
    });

    await test.step("Validate specific miners have group1 & group2 in group column", async () => {
      await groupsPage.clickAddGroupButton();
      await groupsPage.waitForModalListToLoad();
      await groupsPage.validateMinerGroupsByIp(minerIps[0], group1Name);
      await groupsPage.validateMinerGroupsByIp(minerIps[1], `${group1Name}, ${group2Name}`);
      await groupsPage.validateMinerGroupsByIp(minerIps[2], `${group1Name}, ${group2Name}`);
      await groupsPage.validateMinerGroupsByIp(minerIps[3], group2Name);
      await groupsPage.closeModal();
    });

    await test.step("Create group3 with miners 2-4", async () => {
      await groupsPage.clickAddGroupButton();
      await groupsPage.inputGroupName(group3Name);
      await groupsPage.waitForModalListToLoad();
      for (const ip of minerIps.slice(2, 5)) {
        await groupsPage.selectMinerByIp(ip);
      }
      await groupsPage.clickSaveInModal();
      await groupsPage.validateTextInToast(`Group "${group3Name}" created`);
      await groupsPage.validateSavedGroupVisible(group3Name);
      await groupsPage.validateSavedGroupMinerCount(group3Name, 3);
    });

    await test.step("Validate specific miners have group1, group2 & group3 in group column", async () => {
      await groupsPage.clickAddGroupButton();
      await groupsPage.waitForModalListToLoad();
      await groupsPage.validateMinerGroupsByIp(minerIps[0], group1Name);
      await groupsPage.validateMinerGroupsByIp(minerIps[1], `${group1Name}, ${group2Name}`);
      await groupsPage.validateMinerGroupsByIp(minerIps[2], `${group1Name}, ${group2Name}, ${group3Name}`);
      await groupsPage.validateMinerGroupsByIp(minerIps[3], `${group2Name}, ${group3Name}`);
      await groupsPage.validateMinerGroupsByIp(minerIps[4], group3Name);
      await groupsPage.closeModal();
    });

    await test.step("Validate each group filter shows correct miners", async () => {
      await groupsPage.clickAddGroupButton();
      await groupsPage.waitForModalListToLoad();

      await groupsPage.filterModalGroup(group1Name);
      await groupsPage.waitForModalListToLoad();
      await groupsPage.validateOnlyTheseIpsVisibleInModal([minerIps[0], minerIps[1], minerIps[2]]);

      await groupsPage.filterModalGroup(group2Name);
      await groupsPage.waitForModalListToLoad();
      await groupsPage.validateOnlyTheseIpsVisibleInModal([minerIps[1], minerIps[2], minerIps[3]]);

      await groupsPage.filterModalGroup(group3Name);
      await groupsPage.waitForModalListToLoad();
      await groupsPage.validateOnlyTheseIpsVisibleInModal([minerIps[2], minerIps[3], minerIps[4]]);

      await groupsPage.closeModal();
    });

    await test.step("Delete group2", async () => {
      await groupsPage.openSavedGroup(group2Name);
      await groupsPage.clickDeleteGroupInModal();
      await groupsPage.validateTitle(`Delete "${group2Name}"?`);
      await groupsPage.clickDeleteConfirm();
      await groupsPage.validateTextInToast(`Group "${group2Name}" deleted`);
      await groupsPage.validateSavedGroupNotVisible(group2Name);
    });

    await test.step("Validate specific miners have group1, group3 in group column", async () => {
      await groupsPage.clickAddGroupButton();
      await groupsPage.waitForModalListToLoad();
      await groupsPage.validateMinerGroupsByIp(minerIps[0], group1Name);
      await groupsPage.validateMinerGroupsByIp(minerIps[1], group1Name);
      await groupsPage.validateMinerGroupsByIp(minerIps[2], `${group1Name}, ${group3Name}`);
      await groupsPage.validateMinerGroupsByIp(minerIps[3], group3Name);
      await groupsPage.validateMinerGroupsByIp(minerIps[4], group3Name);
      await groupsPage.closeModal();
    });
  });

  test("Cannot create group with no title or miners or with duplicate name", async ({ groupsPage }) => {
    const groupName = generateRandomText("automation1");
    const secondGroupName = generateRandomText("automation2");

    await test.step("Try to create a group without a title", async () => {
      await groupsPage.clickAddGroupButton();
      await groupsPage.clickSaveInModal();
    });

    await test.step("Validate missing name error", async () => {
      await groupsPage.validateErrorMessage("Group name is required");
    });

    await test.step("Try to create a group without any miner", async () => {
      await groupsPage.inputGroupName(groupName);
      await groupsPage.clickSaveInModal();
    });

    await test.step("Validate no miners selected error", async () => {
      await groupsPage.validateErrorMessage("Select at least one miner");
    });

    await test.step("Finish creating a valid group", async () => {
      await groupsPage.clickSelectAllCheckboxInModal();
      await groupsPage.clickSaveInModal();
      await groupsPage.validateTextInToast(`Group "${groupName}" created`);
      await groupsPage.validateSavedGroupVisible(groupName);
    });

    await test.step("Try to create a group with an existing group name", async () => {
      await groupsPage.clickAddGroupButton();
      await groupsPage.inputGroupName(groupName);
      await groupsPage.clickSelectAllCheckboxInModal();
      await groupsPage.clickSaveInModal();
    });

    await test.step("Validate duplicate group name error", async () => {
      await groupsPage.validateErrorMessage("A group with this name already exists");
    });

    await test.step("Finish creating a second valid group", async () => {
      await groupsPage.inputGroupName(secondGroupName);
      await groupsPage.clickSaveInModal();
      await groupsPage.validateTextInToast(`Group "${secondGroupName}" created`);
      await groupsPage.validateSavedGroupVisible(groupName);
      await groupsPage.validateSavedGroupVisible(secondGroupName);
    });
  });

  test("Create a group with all miners from Miners page and reboot group from Groups page", async ({
    minersPage,
    groupsPage,
    commonSteps,
  }) => {
    const groupName = generateRandomText("automation");
    let minerCount: number;

    await test.step("Go to miners page", async () => {
      await commonSteps.goToMinersPage();
    });

    await test.step("Select all miners and create group", async () => {
      minerCount = await minersPage.getMinersCount();
      await minersPage.clickSelectAllCheckbox();
      await minersPage.clickActionsMenuButton();
      await minersPage.clickAddToGroupButton();
      await minersPage.inputNewGroupName(groupName);
      await minersPage.clickSaveInModal();
    });

    await test.step("Validate group creation success", async () => {
      await minersPage.validateTextInToast(`Added ${minerCount} miners to group`);
    });

    await test.step("Reload page (workaround for DASH-1435)", async () => {
      await minersPage.reloadPage();
      await minersPage.waitForMinersTitle();
      await minersPage.waitForMinersListToLoad();
    });

    await test.step("Validate group name in group column for all miners", async () => {
      const currentMinerCount = await minersPage.getMinersCount();
      for (let i = 0; i < currentMinerCount; i++) {
        const minerIp = await minersPage.getMinerIpAddressByIndex(i);
        await minersPage.validateMinerGroupName(minerIp, groupName);
      }
    });

    await test.step("Navigate to groups page and validate group", async () => {
      await groupsPage.navigateToGroupsPage();
      await groupsPage.validateSavedGroupVisible(groupName);
      await groupsPage.validateSavedGroupMinerCount(groupName, minerCount);
    });

    await test.step("Reboot group from Groups page", async () => {
      await groupsPage.clickGroupActionsButton(groupName);
      await groupsPage.clickRebootGroupButton();
      await groupsPage.validateRebootConfirmationModal(minerCount);
      await groupsPage.clickRebootConfirm();
    });

    await test.step("Validate reboot success", async () => {
      await groupsPage.validateTextInToastGroup(`Rebooted ${minerCount} out of ${minerCount} miners`);
    });

    await test.step("Navigate to miners page and validate rebooting status", async () => {
      await commonSteps.goToMinersPage();
      await minersPage.validateAllMinersStatus("Rebooting");
    });

    await test.step("Wait for Hashing status (reduce risk of causing issues to the next test)", async () => {
      await minersPage.validateNoMinerWithStatus("Rebooting");
    });
  });

  test("Group overview actions menu manages power for selected rig miners", async ({
    groupsPage,
    minersPage,
    page,
  }) => {
    const groupName = generateRandomText("automation");
    let minerCount = 0;

    await test.step("Create a rig-only group with two miners", async () => {
      await groupsPage.clickAddGroupButton();
      await groupsPage.inputGroupName(groupName);
      await groupsPage.waitForModalListToLoad();
      await groupsPage.filterModalType(PROTO_RIG_MODEL);
      await groupsPage.waitForModalListToLoad();

      minerCount = 2;
      await groupsPage.selectMinersByIndex([0, 1]);
      await groupsPage.clickSaveInModal();

      await groupsPage.validateTextInToast(`Group "${groupName}" created`);
      await groupsPage.validateSavedGroupVisible(groupName);
      await groupsPage.validateSavedGroupMinerCount(groupName, minerCount);
    });

    await test.step("Open the group overview", async () => {
      await groupsPage.openSavedGroupOverview(groupName);
    });

    const requestPromise = page.waitForRequest(/SetPowerTarget/);
    const responsePromise = page.waitForResponse(/SetPowerTarget/);

    await test.step("Use the overview actions menu to reduce power", async () => {
      await groupsPage.openGroupOverviewActionsMenu();
      await groupsPage.clickGroupOverviewManagePower();
      await minersPage.clickReducePowerOption();
      await minersPage.clickManagePowerConfirm();
    });

    await test.step("Validate manage power toasts", async () => {
      await groupsPage.validateTextInToastGroup("Updating power settings");
      await groupsPage.validateTextInToastGroup("Updated power settings");
    });

    await test.step("Validate the SetPowerTarget request targets the grouped miners", async () => {
      const request = await requestPromise;
      const response = await responsePromise;
      const requestBody = request.postDataJSON();

      test.expect(request.method()).toBe("POST");
      test.expect(requestBody).toHaveProperty("performanceMode");
      test.expect(requestBody.performanceMode).toBe("PERFORMANCE_MODE_EFFICIENCY");
      test.expect(requestBody).toHaveProperty("deviceSelector");
      test.expect(requestBody.deviceSelector).toHaveProperty("includeDevices");
      test.expect(requestBody.deviceSelector.includeDevices).toHaveProperty("deviceIdentifiers");
      test.expect(requestBody.deviceSelector.includeDevices.deviceIdentifiers).toHaveLength(minerCount);
      test.expect(response.status()).toBe(200);
    });
  });
});
