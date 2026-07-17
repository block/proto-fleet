import { testConfig } from "../config/test.config";
import { expect, test } from "../fixtures/pageFixtures";
import {
  ADMIN_RBAC_API_KEY_PREFIX,
  cleanupAdminApiKeys,
  mockAdminServerLogs,
  mockManageableNodes,
  mockReadOnlyNodes,
  provisionAdminRole,
} from "../helpers/rbacAdminTestSetup";
import { cleanupRbacTeamArtifacts, RBAC_ROLE_PREFIX } from "../helpers/rbacTestSetup";
import { generateRandomText } from "../helpers/testDataHelper";

test.describe("Proto Fleet - Admin RBAC", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test.afterAll("CLEANUP: delete admin RBAC fixtures", async ({ browser }, testInfo) => {
    await test.step("Clean up RBAC admin API keys", async () => {
      await cleanupAdminApiKeys(
        browser,
        testInfo.project.use?.isMobile ?? false,
        testInfo.project.use?.viewport ?? null,
      );
    });

    await test.step("Clean up RBAC team fixtures", async () => {
      await cleanupRbacTeamArtifacts(browser, testInfo);
    });
  });

  test("Activity read role can view the activity log and export CSV", async ({
    activityPage,
    browser,
    commonSteps,
  }) => {
    await test.step("Provision an activity-read role", async () => {
      await provisionAdminRole(browser, test.info(), commonSteps, {
        roleDescription: "Read the organization activity log for RBAC coverage.",
        permissionKeys: ["activity:read"],
      });
    });

    await test.step("Open Activity and validate the feed loads", async () => {
      await activityPage.navigateToActivityPage();
      await activityPage.waitForActivityListToLoad();
    });

    await test.step("Export the activity feed as CSV", async () => {
      const download = await activityPage.exportCsv();
      expect(download.suggestedFilename()).toMatch(/activity-export.*\.csv$/i);
    });
  });

  test("Server-log read role can access recent server logs", async ({ browser, commonSteps, page, serverLogsPage }) => {
    await test.step("Mock the Server Logs backend data", async () => {
      await mockAdminServerLogs(page);
    });

    await test.step("Provision a server-log-read role", async () => {
      await provisionAdminRole(browser, test.info(), commonSteps, {
        roleDescription: "Read server logs for RBAC coverage.",
        permissionKeys: ["serverlog:read"],
      });
    });

    await test.step("Open Server Logs and validate the mocked feed", async () => {
      await serverLogsPage.navigateToServerLogsSettings();
      await serverLogsPage.validateServerLogsPageOpened();
      await serverLogsPage.waitForLogRowCount(2);
      await serverLogsPage.validateLogRowVisible("fleetd server booted");
      await serverLogsPage.validateLogRowVisible("scheduler node disconnected");
    });
  });

  test("Fleet-node read role can view nodes without enrollment controls", async ({
    browser,
    commonSteps,
    page,
    settingsNodesPage,
  }) => {
    await test.step("Mock the Nodes backend data", async () => {
      await mockReadOnlyNodes(page);
    });

    await test.step("Provision a fleet-node-read role", async () => {
      await provisionAdminRole(browser, test.info(), commonSteps, {
        roleDescription: "View nodes without enrollment controls for RBAC coverage.",
        permissionKeys: ["fleetnode:read"],
      });
    });

    await test.step("Open Nodes and validate management controls stay hidden", async () => {
      await settingsNodesPage.navigateToNodesSettings();
      await settingsNodesPage.waitForNodesListToLoad();
      await settingsNodesPage.validateNodeVisible("node-01");
      await settingsNodesPage.validateEnrollNodeHidden();
      await settingsNodesPage.validateNodeActionHidden("Confirm enrollment");
      await settingsNodesPage.validateNodeActionHidden("Revoke");
    });
  });

  test("Fleet-node manage role can open enrollment and confirmation controls", async ({
    browser,
    commonSteps,
    page,
    settingsNodesPage,
  }) => {
    const nodesMock = await test.step("Mock the Nodes backend data and enrollment flow", async () => {
      return await mockManageableNodes(page);
    });

    await test.step("Provision a fleet-node-manage role", async () => {
      await provisionAdminRole(browser, test.info(), commonSteps, {
        roleDescription: "Manage nodes for RBAC coverage.",
        permissionKeys: ["fleetnode:read", "fleetnode:manage"],
      });
    });

    await test.step("Open Nodes and start a new enrollment", async () => {
      await settingsNodesPage.navigateToNodesSettings();
      await settingsNodesPage.waitForNodesListToLoad();
      await settingsNodesPage.validateEnrollNodeVisible();
      await settingsNodesPage.clickEnrollNode();
      await settingsNodesPage.validateEnrollNodeModalOpened();
      await page.keyboard.press("Escape");
      nodesMock.showAwaitingNode();
    });

    await test.step("Open the pending-node confirmation controls", async () => {
      await settingsNodesPage.reloadPage();
      await settingsNodesPage.waitForNodesListToLoad();
      await settingsNodesPage.clickNodeActionsMenu("node-pending");
      await settingsNodesPage.validateNodeActionVisible("Confirm enrollment");
      await settingsNodesPage.validateNodeActionVisible("Revoke");
      await settingsNodesPage.clickNodeAction("Confirm enrollment");
      await settingsNodesPage.validateConfirmNodeModalOpened("node-pending");
    });
  });

  test("API-key manage role can create and revoke API keys", async ({ browser, commonSteps, settingsApiKeysPage }) => {
    const apiKeyName = generateRandomText(ADMIN_RBAC_API_KEY_PREFIX);

    await test.step("Provision an API-key-manage role", async () => {
      await provisionAdminRole(browser, test.info(), commonSteps, {
        roleDescription: "Manage API keys for RBAC coverage.",
        permissionKeys: ["apikey:manage"],
      });
    });

    await test.step("Open Integrations and create an API key", async () => {
      await settingsApiKeysPage.navigateToApiKeysSettings();
      await settingsApiKeysPage.validateApiKeysPageOpened();
      await settingsApiKeysPage.clickCreateApiKey();
      await settingsApiKeysPage.inputApiKeyName(apiKeyName);
      await settingsApiKeysPage.clickCreateInModal();
      await settingsApiKeysPage.validateApiKeyCreated();
      await settingsApiKeysPage.clickDone();
      await settingsApiKeysPage.validateApiKeyVisible(apiKeyName);
    });

    await test.step("Revoke the API key", async () => {
      await settingsApiKeysPage.clickRevokeApiKey(apiKeyName);
      await settingsApiKeysPage.confirmRevokeApiKey();
      await settingsApiKeysPage.validateApiKeyNotVisible(apiKeyName);
    });
  });

  test("User-read role can list users without management controls", async ({
    browser,
    commonSteps,
    settingsPage,
    settingsTeamPage,
  }) => {
    await test.step("Provision a user-read role", async () => {
      await provisionAdminRole(browser, test.info(), commonSteps, {
        roleDescription: "Read team members without management controls for RBAC coverage.",
        permissionKeys: ["user:read"],
      });
    });

    await test.step("Open Team and validate the members list is read-only", async () => {
      await settingsPage.navigateToTeamSettings();
      await settingsTeamPage.validateTeamSettingsPageOpened();
      await settingsTeamPage.validateMemberVisible(testConfig.users.admin.username);
      await settingsTeamPage.validateAddTeamMemberHidden();
      await settingsTeamPage.validateMemberActionsHidden(testConfig.users.admin.username);
      await settingsTeamPage.validateRolesTabHidden();
    });
  });

  test("User-manage role can create, reset, reassign, and deactivate users", async ({
    browser,
    commonSteps,
    settingsPage,
    settingsTeamPage,
  }) => {
    const createdUsername = generateRandomText("rbac_user_manage_member");
    const baseMemberRole = generateRandomText(RBAC_ROLE_PREFIX);
    const editedMemberRole = generateRandomText(RBAC_ROLE_PREFIX);

    await test.step("Create assignable member roles as admin", async () => {
      await commonSteps.loginAsAdmin({ forceReauth: true });
      await settingsPage.navigateToTeamSettings();
      await settingsTeamPage.validateTeamSettingsPageOpened();
      await settingsTeamPage.openRolesTab();
      await settingsTeamPage.createCustomRole(baseMemberRole, "Base assignable RBAC member role.", ["activity:read"]);
      await settingsTeamPage.createCustomRole(editedMemberRole, "Edited assignable RBAC member role.", [
        "activity:read",
        "user:read",
      ]);
    });

    await test.step("Provision a user-manage role", async () => {
      await provisionAdminRole(browser, test.info(), commonSteps, {
        roleDescription: "Manage users for RBAC coverage.",
        permissionKeys: ["activity:read", "user:read", "user:manage"],
      });
    });

    await test.step("Create a team member with an assignable custom role", async () => {
      await settingsPage.navigateToTeamSettings();
      await settingsTeamPage.validateTeamSettingsPageOpened();
      await settingsTeamPage.openMembersTab();
      await settingsTeamPage.createTeamMemberAndGetTemporaryPassword(createdUsername, baseMemberRole);
      await settingsTeamPage.validateMemberRole(createdUsername, baseMemberRole);
    });

    await test.step("Reset the member password", async () => {
      await settingsTeamPage.clickMemberActionsMenu(createdUsername);
      await settingsTeamPage.clickResetPassword();
      await settingsTeamPage.clickResetMemberPasswordConfirm();
      await settingsTeamPage.validatePasswordReset();
      await settingsTeamPage.clickDone();
    });

    await test.step("Reassign the member role", async () => {
      await settingsTeamPage.clickMemberActionsMenu(createdUsername);
      await settingsTeamPage.clickEditRole();
      await settingsTeamPage.selectEditedRole(editedMemberRole);
      await settingsTeamPage.clickSaveEditedRole();
      await settingsTeamPage.validateMemberRole(createdUsername, editedMemberRole);
    });

    await test.step("Deactivate the member", async () => {
      await settingsTeamPage.clickMemberActionsMenu(createdUsername);
      await settingsTeamPage.clickDeactivate();
      await settingsTeamPage.clickConfirmDeactivation();
      await settingsTeamPage.validateMemberNotInList(createdUsername);
    });
  });

  test("Role-manage role can create, edit, and delete custom roles while built-in roles stay immutable", async ({
    browser,
    commonSteps,
    settingsPage,
    settingsTeamPage,
  }) => {
    const roleName = generateRandomText(RBAC_ROLE_PREFIX);
    const updatedRoleName = generateRandomText(RBAC_ROLE_PREFIX);

    await test.step("Provision a role-manage role", async () => {
      await provisionAdminRole(browser, test.info(), commonSteps, {
        roleDescription: "Manage roles for RBAC coverage.",
        permissionKeys: ["activity:read", "role:manage"],
      });
    });

    await test.step("Open Team roles and validate built-in roles are immutable", async () => {
      await settingsPage.navigateToTeamSettings();
      await settingsTeamPage.validateTeamSettingsPageOpened();
      await settingsTeamPage.openRolesTab();
      await settingsTeamPage.validateSystemRoleLockVisible();
    });

    await test.step("Create a custom role", async () => {
      await settingsTeamPage.createCustomRole(roleName, "Custom RBAC role under test.", ["activity:read"]);
    });

    await test.step("Edit the custom role", async () => {
      await settingsTeamPage.clickRoleActionsMenu(roleName);
      await settingsTeamPage.clickEditRoleAction();
      await settingsTeamPage.inputRoleName(updatedRoleName);
      await settingsTeamPage.inputRoleDescription("Updated RBAC role description.");
      await settingsTeamPage.clickSaveRoleChanges();
      await settingsTeamPage.validateRoleVisible(updatedRoleName);
      await settingsTeamPage.validateRoleNotVisible(roleName);
    });

    await test.step("Delete the custom role", async () => {
      await settingsTeamPage.clickRoleActionsMenu(updatedRoleName);
      await settingsTeamPage.clickDeleteRoleAction();
      await settingsTeamPage.clickDeleteRoleConfirm();
      await settingsTeamPage.validateRoleNotVisible(updatedRoleName);
    });
  });
});
