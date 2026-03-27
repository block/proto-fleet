/* eslint-disable playwright/expect-expect */
import { test } from "../fixtures/pageFixtures";
import { PROTO_RIG_MODEL } from "../helpers/minerModels";
import { generateRandomText } from "../helpers/testDataHelper";
import { GroupsPage } from "../pages/groups";

test.describe("Groups", () => {
  const testGroupPrefixes = ["group_", "group1_", "group2_", "group3_", "edited-group_"];

  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  async function cleanupLeakedTestGroups(groupsPage: GroupsPage) {
    while (true) {
      const existingGroupNames = await groupsPage.listSavedGroupNames();
      const leakedGroupNames = existingGroupNames.filter((groupName: string) =>
        testGroupPrefixes.some((prefix) => groupName.startsWith(prefix)),
      );

      if (leakedGroupNames.length === 0) {
        return;
      }

      for (const groupName of leakedGroupNames) {
        await groupsPage.openSavedGroup(groupName);
        await groupsPage.clickDeleteGroupInModal();
        await groupsPage.clickDeleteConfirm();
        await groupsPage.validateSavedGroupNotVisible(groupName);
      }
    }
  }

  test("Create, edit, and delete groups @smoke", async ({ groupsPage, commonSteps }) => {
    const groupName = generateRandomText("group");
    const editedGroupName = generateRandomText("edited-group");

    await commonSteps.loginAsAdmin();

    await test.step("Navigate to Groups page", async () => {
      await groupsPage.navigateToGroupsPage();
      await cleanupLeakedTestGroups(groupsPage);
    });

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

  test("Validate groups association to miners", async ({ groupsPage, commonSteps }) => {
    const group1Name = generateRandomText("group1");
    const group2Name = generateRandomText("group2");
    const group3Name = generateRandomText("group3");
    const createdGroups: string[] = [];

    await commonSteps.loginAsAdmin();

    const minerIps: string[] = [];
    try {
      await test.step("Navigate to Groups page", async () => {
        await groupsPage.navigateToGroupsPage();
        await cleanupLeakedTestGroups(groupsPage);
      });

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
        createdGroups.push(group1Name);
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
        createdGroups.push(group2Name);
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
        createdGroups.push(group3Name);
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
        createdGroups.splice(createdGroups.indexOf(group2Name), 1);
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
    } finally {
      for (const groupName of [...createdGroups].reverse()) {
        try {
          await groupsPage.navigateToGroupsPage();
          await groupsPage.deleteSavedGroupIfVisible(groupName);
        } catch {
          // Best-effort cleanup to keep later runs isolated.
        }
      }
    }
  });

  test("Cannot create group with no title or miners or with duplicate name", async ({ groupsPage, commonSteps }) => {
    const groupName = generateRandomText("group");
    const secondGroupName = generateRandomText("group");

    await commonSteps.loginAsAdmin();

    await test.step("Navigate to Groups page", async () => {
      await groupsPage.navigateToGroupsPage();
      await cleanupLeakedTestGroups(groupsPage);
    });

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
      await groupsPage.validateErrorMessage("[already_exists] a collection with this name already exists");
    });

    await test.step("Finish creating a second valid group", async () => {
      await groupsPage.inputGroupName(secondGroupName);
      await groupsPage.clickSaveInModal();
      await groupsPage.validateTextInToast(`Group "${secondGroupName}" created`);
      await groupsPage.validateSavedGroupVisible(groupName);
      await groupsPage.validateSavedGroupVisible(secondGroupName);
    });
  });
});
