import { create, toJsonString } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import { type Browser, type Route } from "@playwright/test";
import { testConfig } from "../config/test.config";
import { expect, test } from "../fixtures/pageFixtures";
import { CommonSteps } from "../helpers/commonSteps";
import {
  cleanupRbacTeamArtifacts,
  provisionRoleAndLoginViaStoredAdminContext,
  RBAC_ROLE_PREFIX,
} from "../helpers/rbacTestSetup";
import { generateRandomText } from "../helpers/testDataHelper";
import { AuthPage } from "../pages/auth";
import { MinersPage } from "../pages/miners";
import { SettingsPage } from "../pages/settings";
import { SettingsApiKeysPage } from "../pages/settingsApiKeys";
import { SettingsTeamPage } from "../pages/settingsTeam";
import {
  CreateEnrollmentCodeResponseSchema,
  FleetNodeEnrollmentStatus,
  FleetNodeSummarySchema,
  ListFleetNodesResponseSchema,
} from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import {
  ListServerLogsResponseSchema,
  LogEntrySchema,
  LogLevel,
} from "@/protoFleet/api/generated/serverlog/v1/serverlog_pb";

const ADMIN_RBAC_API_KEY_PREFIX = "rbac_admin_api_key";
const NODES_RPC_PATTERN = /FleetNodeAdminService\/ListFleetNodes/;
const CREATE_ENROLLMENT_CODE_RPC_PATTERN = /FleetNodeAdminService\/CreateEnrollmentCode/;
const SERVER_LOGS_RPC_PATTERN = /ServerLogService\/ListServerLogs/;

async function provisionAdminRole(
  browser: Browser,
  commonSteps: Parameters<typeof provisionRoleAndLoginViaStoredAdminContext>[2],
  {
    permissionKeys,
    roleDescription,
  }: {
    permissionKeys: string[];
    roleDescription: string;
  },
) {
  return await provisionRoleAndLoginViaStoredAdminContext(browser, test.info(), commonSteps, {
    permissionKeys,
    roleDescription,
  });
}

function createTimestamp(date: Date) {
  return create(TimestampSchema, {
    seconds: BigInt(Math.floor(date.getTime() / 1000)),
    nanos: 0,
  });
}

function createNodeSummary({
  fleetNodeId,
  pendingEnrollmentId,
  name,
  enrollmentStatus,
  createdAt,
  lastSeenAt,
  identityFingerprint,
}: {
  fleetNodeId: bigint;
  pendingEnrollmentId?: bigint;
  name: string;
  enrollmentStatus: FleetNodeEnrollmentStatus;
  createdAt?: Date;
  lastSeenAt?: Date;
  identityFingerprint: string;
}) {
  return create(FleetNodeSummarySchema, {
    fleetNodeId,
    pendingEnrollmentId,
    name,
    enrollmentStatus,
    createdAt: createdAt ? createTimestamp(createdAt) : undefined,
    lastSeenAt: lastSeenAt ? createTimestamp(lastSeenAt) : undefined,
    identityFingerprint,
  });
}

function fulfillFleetNodes(route: Route, nodes: ReturnType<typeof createNodeSummary>[]) {
  return route.fulfill({
    status: 200,
    contentType: "application/json",
    body: toJsonString(
      ListFleetNodesResponseSchema,
      create(ListFleetNodesResponseSchema, {
        fleetNodes: nodes,
      }),
    ),
  });
}

function fulfillEnrollmentCode(route: Route) {
  return route.fulfill({
    status: 200,
    contentType: "application/json",
    body: toJsonString(
      CreateEnrollmentCodeResponseSchema,
      create(CreateEnrollmentCodeResponseSchema, {
        code: "rbac-enrollment-code-1234",
        pendingEnrollmentId: 11n,
        expiresAt: createTimestamp(new Date("2026-07-17T10:00:00Z")),
      }),
    ),
  });
}

function createServerLogEntry({
  id,
  level,
  message,
  source,
  time,
}: {
  id: bigint;
  level: LogLevel;
  message: string;
  source: string;
  time: Date;
}) {
  return create(LogEntrySchema, {
    id,
    level,
    message,
    source,
    time: createTimestamp(time),
  });
}

async function cleanupAdminApiKeys(
  browser: Browser,
  isMobile: boolean,
  viewport: { height: number; width: number } | null,
) {
  const context = await browser.newContext({
    baseURL: testConfig.baseUrl,
    viewport: viewport ?? undefined,
  });

  try {
    const page = await context.newPage();
    await page.goto("/");

    const authPage = new AuthPage(page, isMobile);
    const minersPage = new MinersPage(page, isMobile);
    const settingsPage = new SettingsPage(page, isMobile);
    const settingsTeamPage = new SettingsTeamPage(page, isMobile);
    const settingsApiKeysPage = new SettingsApiKeysPage(page, isMobile);
    const commonSteps = new CommonSteps(authPage, minersPage, settingsPage, settingsTeamPage);

    await commonSteps.loginAsAdmin({ forceReauth: true });
    await settingsApiKeysPage.navigateToApiKeysSettings();
    await settingsApiKeysPage.deleteApiKeysByPrefix(ADMIN_RBAC_API_KEY_PREFIX);
  } finally {
    await context.close();
  }
}

test.describe("Proto Fleet - Admin RBAC", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test.afterAll("CLEANUP: delete admin RBAC fixtures", async ({ browser }, testInfo) => {
    await cleanupRbacTeamArtifacts(browser, testInfo);
    await cleanupAdminApiKeys(browser, testInfo.project.use?.isMobile ?? false, testInfo.project.use?.viewport ?? null);
  });

  test("Activity read role can view the activity log and export CSV", async ({
    activityPage,
    browser,
    commonSteps,
  }) => {
    await test.step("Provision an activity-read role", async () => {
      await provisionAdminRole(browser, commonSteps, {
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
    await page.route(SERVER_LOGS_RPC_PATTERN, async (route) => {
      return await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: toJsonString(
          ListServerLogsResponseSchema,
          create(ListServerLogsResponseSchema, {
            entries: [
              createServerLogEntry({
                id: 1n,
                level: LogLevel.INFO,
                message: "server booted",
                source: "fleetd",
                time: new Date("2026-07-17T09:00:00Z"),
              }),
              createServerLogEntry({
                id: 2n,
                level: LogLevel.WARN,
                message: "node disconnected",
                source: "scheduler",
                time: new Date("2026-07-17T09:00:05Z"),
              }),
            ],
            latestId: 2n,
            bufferSize: 2,
            truncated: false,
          }),
        ),
      });
    });

    await test.step("Provision a server-log-read role", async () => {
      await provisionAdminRole(browser, commonSteps, {
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
    await page.route(NODES_RPC_PATTERN, async (route) => {
      return await fulfillFleetNodes(route, [
        createNodeSummary({
          fleetNodeId: 7n,
          name: "node-01",
          enrollmentStatus: FleetNodeEnrollmentStatus.CONFIRMED,
          identityFingerprint: "SHA256:rbac-node-01",
          createdAt: new Date("2026-07-17T08:00:00Z"),
          lastSeenAt: new Date("2026-07-17T09:00:00Z"),
        }),
      ]);
    });

    await test.step("Provision a fleet-node-read role", async () => {
      await provisionAdminRole(browser, commonSteps, {
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
    let showAwaitingNode = false;

    await page.route(NODES_RPC_PATTERN, async (route) => {
      return await fulfillFleetNodes(
        route,
        showAwaitingNode
          ? [
              createNodeSummary({
                fleetNodeId: 11n,
                pendingEnrollmentId: 11n,
                name: "node-pending",
                enrollmentStatus: FleetNodeEnrollmentStatus.AWAITING_CONFIRMATION,
                identityFingerprint: "SHA256:rbac-pending-node",
                lastSeenAt: new Date("2026-07-17T09:05:00Z"),
              }),
            ]
          : [
              createNodeSummary({
                fleetNodeId: 7n,
                name: "node-01",
                enrollmentStatus: FleetNodeEnrollmentStatus.CONFIRMED,
                identityFingerprint: "SHA256:rbac-node-01",
                createdAt: new Date("2026-07-17T08:00:00Z"),
                lastSeenAt: new Date("2026-07-17T09:00:00Z"),
              }),
            ],
      );
    });
    await page.route(CREATE_ENROLLMENT_CODE_RPC_PATTERN, async (route) => {
      return await fulfillEnrollmentCode(route);
    });

    await test.step("Provision a fleet-node-manage role", async () => {
      await provisionAdminRole(browser, commonSteps, {
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
      showAwaitingNode = true;
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
      await provisionAdminRole(browser, commonSteps, {
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
      await provisionAdminRole(browser, commonSteps, {
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
      await provisionAdminRole(browser, commonSteps, {
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
      await provisionAdminRole(browser, commonSteps, {
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
