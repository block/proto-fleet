import { create, toJsonString } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import { type Browser, type Page, type Route, type TestInfo } from "@playwright/test";
import { testConfig } from "../config/test.config";
import { AuthPage } from "../pages/auth";
import { MinersPage } from "../pages/miners";
import { SettingsPage } from "../pages/settings";
import { SettingsApiKeysPage } from "../pages/settingsApiKeys";
import { SettingsTeamPage } from "../pages/settingsTeam";
import { CommonSteps } from "./commonSteps";
import { provisionRoleAndLoginViaStoredAdminContext } from "./rbacTestSetup";
import { createServerLogEntry, fulfillServerLogs } from "./serverLogsMocks";
import {
  CreateEnrollmentCodeResponseSchema,
  FleetNodeEnrollmentStatus,
  FleetNodeSummarySchema,
  ListFleetNodesResponseSchema,
} from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import { LogLevel } from "@/protoFleet/api/generated/serverlog/v1/serverlog_pb";

export const ADMIN_RBAC_API_KEY_PREFIX = "rbac_admin_api_key";

const NODES_RPC_PATTERN = /FleetNodeAdminService\/ListFleetNodes/;
const CREATE_ENROLLMENT_CODE_RPC_PATTERN = /FleetNodeAdminService\/CreateEnrollmentCode/;
const SERVER_LOGS_RPC_PATTERN = /ServerLogService\/ListServerLogs/;

type NodesMockController = {
  showAwaitingNode: () => void;
};

export async function provisionAdminRole(
  browser: Browser,
  testInfo: TestInfo,
  commonSteps: Parameters<typeof provisionRoleAndLoginViaStoredAdminContext>[2],
  {
    permissionKeys,
    roleDescription,
  }: {
    permissionKeys: string[];
    roleDescription: string;
  },
) {
  return await provisionRoleAndLoginViaStoredAdminContext(browser, testInfo, commonSteps, {
    permissionKeys,
    roleDescription,
  });
}

export async function cleanupAdminApiKeys(
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

export async function mockAdminServerLogs(page: Page) {
  await page.route(SERVER_LOGS_RPC_PATTERN, async (route) => {
    return await fulfillServerLogs(
      route,
      [
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
      2n,
    );
  });
}

export async function mockReadOnlyNodes(page: Page) {
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
}

export async function mockManageableNodes(page: Page): Promise<NodesMockController> {
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
    return await route.fulfill({
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
  });

  return {
    showAwaitingNode() {
      showAwaitingNode = true;
    },
  };
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
