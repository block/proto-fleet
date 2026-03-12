/* eslint-disable playwright/expect-expect */
import { test } from "../fixtures/pageFixtures";

test.describe("Groups", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test("Create, edit, and delete groups", async ({ groupsPage, commonSteps }) => {
    await commonSteps.loginAsAdmin();

    await test.step("Navigate to Groups page", async () => {
      await groupsPage.navigateToGroupsPage();
    });

    await test.step("Create new group with all miners", async () => {
      await groupsPage.clickAddGroupButton();
      await groupsPage.inputGroupName("test");

      await groupsPage.waitForModalListToLoad();
      const allMinersCount = await groupsPage.getModalListRowCount();

      await groupsPage.clickSelectAllCheckboxInModal();
      await groupsPage.clickSaveInModal();

      await groupsPage.validateTextInToast('Group "test" created');
      await groupsPage.validateSavedGroupVisible("test");
      await groupsPage.validateSavedGroupMinerCount("test", allMinersCount);
    });

    await test.step("Edit group to only rig miners", async () => {
      await groupsPage.openSavedGroup("test");
      await groupsPage.waitForModalListToLoad();

      await groupsPage.inputGroupName("edited-group");

      // clear previous selection
      await groupsPage.clickSelectAllCheckboxInModal();

      await groupsPage.filterModalType("Rig");
      await groupsPage.waitForModalListToLoad();

      await groupsPage.clickSelectAllCheckboxInModal();
      const rigMinersCount = await groupsPage.getModalListRowCount();

      await groupsPage.clickSaveInModal();

      await groupsPage.validateTextInToast('Group "edited-group" updated');
      await groupsPage.validateSavedGroupVisible("edited-group");
      await groupsPage.validateSavedGroupMinerCount("edited-group", rigMinersCount);
    });

    await test.step("Delete group", async () => {
      await groupsPage.openSavedGroup("edited-group");
      await groupsPage.clickDeleteGroupInModal();
      await groupsPage.validateTitle(`Delete "edited-group"?`);
      await groupsPage.clickDeleteConfirm();

      await groupsPage.validateTextInToast('Group "edited-group" deleted');
      await groupsPage.validateSavedGroupNotVisible("edited-group");
    });
  });

  test("Validate groups association to miners", async ({ groupsPage, commonSteps }) => {
    await commonSteps.loginAsAdmin();

    const minerIps: string[] = [];

    await test.step("Navigate to Groups page", async () => {
      await groupsPage.navigateToGroupsPage();
    });

    await test.step("Capture miners 0-4 identifiers", async () => {
      await groupsPage.clickAddGroupButton();
      await groupsPage.waitForModalListToLoad();
      for (let i = 0; i < 5; i++) {
        minerIps.push(await groupsPage.getModalRowIpAddressByIndex(i));
      }
      await groupsPage.closeModal();
    });

    await test.step("Create group1 with miners 0-2", async () => {
      await groupsPage.clickAddGroupButton();
      await groupsPage.inputGroupName("group1");
      await groupsPage.waitForModalListToLoad();
      await groupsPage.selectMinersByIndex([0, 1, 2]);
      await groupsPage.clickSaveInModal();
      await groupsPage.validateTextInToast('Group "group1" created');
      await groupsPage.validateSavedGroupVisible("group1");
      await groupsPage.validateSavedGroupMinerCount("group1", 3);
    });

    await test.step("Validate specific miners have group1 in group column", async () => {
      await groupsPage.clickAddGroupButton();
      await groupsPage.waitForModalListToLoad();
      await groupsPage.validateMinerGroupsByIndex(0, "group1");
      await groupsPage.validateMinerGroupsByIndex(1, "group1");
      await groupsPage.validateMinerGroupsByIndex(2, "group1");
      await groupsPage.closeModal();
    });

    await test.step("Create group2 with miners 1-3", async () => {
      await groupsPage.clickAddGroupButton();
      await groupsPage.inputGroupName("group2");
      await groupsPage.waitForModalListToLoad();
      await groupsPage.selectMinersByIndex([1, 2, 3]);
      await groupsPage.clickSaveInModal();
      await groupsPage.validateTextInToast('Group "group2" created');
      await groupsPage.validateSavedGroupVisible("group2");
      await groupsPage.validateSavedGroupMinerCount("group2", 3);
    });

    await test.step("Validate specific miners have group1 & group2 in group column", async () => {
      await groupsPage.clickAddGroupButton();
      await groupsPage.waitForModalListToLoad();
      await groupsPage.validateMinerGroupsByIndex(0, "group1");
      await groupsPage.validateMinerGroupsByIndex(1, "group1, group2");
      await groupsPage.validateMinerGroupsByIndex(2, "group1, group2");
      await groupsPage.validateMinerGroupsByIndex(3, "group2");
      await groupsPage.closeModal();
    });

    await test.step("Create group3 with miners 2-4", async () => {
      await groupsPage.clickAddGroupButton();
      await groupsPage.inputGroupName("group3");
      await groupsPage.waitForModalListToLoad();
      await groupsPage.selectMinersByIndex([2, 3, 4]);
      await groupsPage.clickSaveInModal();
      await groupsPage.validateTextInToast('Group "group3" created');
      await groupsPage.validateSavedGroupVisible("group3");
      await groupsPage.validateSavedGroupMinerCount("group3", 3);
    });

    await test.step("Validate specific miners have group1, group2 & group3 in group column", async () => {
      await groupsPage.clickAddGroupButton();
      await groupsPage.waitForModalListToLoad();
      await groupsPage.validateMinerGroupsByIndex(0, "group1");
      await groupsPage.validateMinerGroupsByIndex(1, "group1, group2");
      await groupsPage.validateMinerGroupsByIndex(2, "group1, group2, group3");
      await groupsPage.validateMinerGroupsByIndex(3, "group2, group3");
      await groupsPage.validateMinerGroupsByIndex(4, "group3");
      await groupsPage.closeModal();
    });

    await test.step("Validate each group filter shows correct miners", async () => {
      await groupsPage.clickAddGroupButton();
      await groupsPage.waitForModalListToLoad();

      await groupsPage.filterModalGroup("group1");
      await groupsPage.waitForModalListToLoad();
      await groupsPage.validateOnlyTheseIpsVisibleInModal([minerIps[0], minerIps[1], minerIps[2]]);

      await groupsPage.filterModalGroup("group2");
      await groupsPage.waitForModalListToLoad();
      await groupsPage.validateOnlyTheseIpsVisibleInModal([minerIps[1], minerIps[2], minerIps[3]]);

      await groupsPage.filterModalGroup("group3");
      await groupsPage.waitForModalListToLoad();
      await groupsPage.validateOnlyTheseIpsVisibleInModal([minerIps[2], minerIps[3], minerIps[4]]);

      await groupsPage.closeModal();
    });

    await test.step("Delete group2", async () => {
      await groupsPage.openSavedGroup("group2");
      await groupsPage.clickDeleteGroupInModal();
      await groupsPage.validateTitle(`Delete "group2"?`);
      await groupsPage.clickDeleteConfirm();
      await groupsPage.validateTextInToast('Group "group2" deleted');
      await groupsPage.validateSavedGroupNotVisible("group2");
    });

    await test.step("Validate specific miners have group1, group3 in group column", async () => {
      await groupsPage.clickAddGroupButton();
      await groupsPage.waitForModalListToLoad();
      await groupsPage.validateMinerGroupsByIndex(0, "group1");
      await groupsPage.validateMinerGroupsByIndex(1, "group1");
      await groupsPage.validateMinerGroupsByIndex(2, "group1, group3");
      await groupsPage.validateMinerGroupsByIndex(3, "group3");
      await groupsPage.validateMinerGroupsByIndex(4, "group3");
      await groupsPage.closeModal();
    });
  });

  test("Cannot create group with no title or miners or with duplicate name", async ({ groupsPage, commonSteps }) => {
    await commonSteps.loginAsAdmin();

    await test.step("Navigate to Groups page", async () => {
      await groupsPage.navigateToGroupsPage();
    });

    await test.step("Try to create a group without a title", async () => {
      await groupsPage.clickAddGroupButton();
      await groupsPage.clickSaveInModal();
    });

    await test.step("Validate missing name error", async () => {
      await groupsPage.validateErrorMessage("Group name is required");
    });

    await test.step("Try to create a group without any miner", async () => {
      await groupsPage.inputGroupName("test");
      await groupsPage.clickSaveInModal();
    });

    await test.step("Validate no miners selected error", async () => {
      await groupsPage.validateErrorMessage("Select at least one miner");
    });

    await test.step("Finish creating a valid group", async () => {
      await groupsPage.clickSelectAllCheckboxInModal();
      await groupsPage.clickSaveInModal();
      await groupsPage.validateTextInToast('Group "test" created');
      await groupsPage.validateSavedGroupVisible("test");
    });

    await test.step("Try to create a group with an existing group name", async () => {
      await groupsPage.clickAddGroupButton();
      await groupsPage.inputGroupName("test");
      await groupsPage.clickSelectAllCheckboxInModal();
      await groupsPage.clickSaveInModal();
    });

    await test.step("Validate duplicate group name error", async () => {
      await groupsPage.validateErrorMessage(
        '[internal] failed to create collection: ERROR: duplicate key value violates unique constraint "uk_device_collection_org_type_label" (SQLSTATE 23505)',
      );
    });

    await test.step("Finish creating a second valid group", async () => {
      await groupsPage.inputGroupName("test1");
      await groupsPage.clickSaveInModal();
      await groupsPage.validateTextInToast('Group "test1" created');
      await groupsPage.validateSavedGroupVisible("test");
      await groupsPage.validateSavedGroupVisible("test1");
    });
  });
});
