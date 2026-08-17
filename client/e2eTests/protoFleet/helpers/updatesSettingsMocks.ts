import { create, fromJsonString, toJsonString } from "@bufbuild/protobuf";
import type { Message } from "@bufbuild/protobuf";
import type { GenMessage } from "@bufbuild/protobuf/codegenv2";
import type { Page, Route } from "@playwright/test";
import type {
  GetUpdateStatusResponse,
  GetUpgradeStatusResponse,
  ReleaseInfo,
  UpgradeOperation,
} from "@/protoFleet/api/generated/instance/v1/updates_pb";
import {
  GetUpdateStatusResponseSchema,
  GetUpgradeStatusResponseSchema,
  ReleaseChannel,
  ReleaseInfoSchema,
  SetReleaseChannelRequestSchema,
  SetReleaseChannelResponseSchema,
  TriggerUpgradeRequestSchema,
  TriggerUpgradeResponseSchema,
  UpgradeOperationSchema,
  UpgradePhase,
} from "@/protoFleet/api/generated/instance/v1/updates_pb";
import {
  FleetInitStatusSchema,
  GetFleetInitStatusResponseSchema,
} from "@/protoFleet/api/generated/onboarding/v1/onboarding_pb";

const GET_FLEET_INIT_STATUS_RPC_PATTERN = /OnboardingService\/GetFleetInitStatus/;
const GET_UPDATE_STATUS_RPC_PATTERN = /InstanceUpdateService\/GetUpdateStatus/;
const SET_RELEASE_CHANNEL_RPC_PATTERN = /InstanceUpdateService\/SetReleaseChannel/;
const GET_UPGRADE_STATUS_RPC_PATTERN = /InstanceUpdateService\/GetUpgradeStatus/;
const TRIGGER_UPGRADE_RPC_PATTERN = /InstanceUpdateService\/TriggerUpgrade/;
const AUTH_STORAGE_KEY = "proto-fleet-auth";
const FORCED_SESSION_EXPIRY = "2026-12-31T00:00:00.000Z";

type MessageOverrides<T> = Omit<Partial<T>, "$typeName" | "$unknown">;

export interface UpdatesSettingsMockController {
  releaseChannelRequests: ReleaseChannel[];
  triggerUpgradeRequests: string[];
}

interface MockUpdatesSettingsOptions {
  initialStatus: GetUpdateStatusResponse;
  initialUpgradeStatus?: GetUpgradeStatusResponse;
  onSetReleaseChannel?: (channel: ReleaseChannel) => GetUpdateStatusResponse;
  onTriggerUpgrade?: (targetVersion: string) => UpgradeOperation | undefined;
}

export const buildReleaseInfo = (overrides?: MessageOverrides<ReleaseInfo>): ReleaseInfo =>
  create(ReleaseInfoSchema, {
    version: "v1.3.0",
    releaseNotesUrl: "https://github.com/block/proto-fleet/releases/tag/v1.3.0",
    prerelease: false,
    ...overrides,
  });

export const buildUpdateStatus = (overrides?: MessageOverrides<GetUpdateStatusResponse>): GetUpdateStatusResponse =>
  create(GetUpdateStatusResponseSchema, {
    currentVersion: "v1.2.0",
    channel: ReleaseChannel.STABLE,
    latestEligible: buildReleaseInfo(),
    updateAvailable: true,
    installCommand: "curl -fsSL https://fleet.example.com/install.sh | sh -s -- v1.3.0",
    oneClickAvailable: false,
    statusAvailable: true,
    ...overrides,
  });

export const buildUpgradeStatus = (overrides?: MessageOverrides<GetUpgradeStatusResponse>): GetUpgradeStatusResponse =>
  create(GetUpgradeStatusResponseSchema, {
    executorAvailable: true,
    ...overrides,
  });

export const buildUpgradeOperation = (
  phase: UpgradePhase,
  overrides?: MessageOverrides<UpgradeOperation>,
): UpgradeOperation =>
  create(UpgradeOperationSchema, {
    id: "operation-1",
    targetVersion: "v1.3.0",
    phase,
    message: "Preparing upgrade",
    ...overrides,
  });

export async function mockUpdatesSettings(
  page: Page,
  {
    initialStatus,
    initialUpgradeStatus = buildUpgradeStatus(),
    onSetReleaseChannel,
    onTriggerUpgrade,
  }: MockUpdatesSettingsOptions,
): Promise<UpdatesSettingsMockController> {
  let currentStatus = initialStatus;
  let currentUpgradeStatus = initialUpgradeStatus;
  const releaseChannelRequests: ReleaseChannel[] = [];
  const triggerUpgradeRequests: string[] = [];

  await page.addInitScript(
    ({ authStorageKey, forcedSessionExpiry }) => {
      const raw = window.localStorage.getItem(authStorageKey);
      const parsed = raw ? (JSON.parse(raw) as { state?: { auth?: Record<string, unknown> }; version?: number }) : {};
      const auth = parsed.state?.auth ?? {};
      const permissions = Array.isArray(auth.permissions)
        ? auth.permissions.filter((value) => typeof value === "string")
        : [];
      const nextPermissions = permissions.includes("instance:update")
        ? permissions
        : [...permissions, "instance:update"];

      window.localStorage.setItem(
        authStorageKey,
        JSON.stringify({
          version: parsed.version ?? 0,
          state: {
            auth: {
              ...auth,
              authLoading: false,
              isAuthenticated: true,
              permissions: nextPermissions,
              sessionExpiry: forcedSessionExpiry,
            },
          },
        }),
      );
    },
    { authStorageKey: AUTH_STORAGE_KEY, forcedSessionExpiry: FORCED_SESSION_EXPIRY },
  );

  await page.route(GET_FLEET_INIT_STATUS_RPC_PATTERN, async (route) => {
    return await fulfill(
      route,
      GetFleetInitStatusResponseSchema,
      create(GetFleetInitStatusResponseSchema, {
        status: create(FleetInitStatusSchema, { adminCreated: true }),
      }),
    );
  });

  await page.route(GET_UPDATE_STATUS_RPC_PATTERN, async (route) => {
    return await fulfill(route, GetUpdateStatusResponseSchema, currentStatus);
  });

  await page.route(SET_RELEASE_CHANNEL_RPC_PATTERN, async (route) => {
    const request = fromJsonString(SetReleaseChannelRequestSchema, route.request().postData() ?? "{}");
    releaseChannelRequests.push(request.channel);
    currentStatus = onSetReleaseChannel?.(request.channel) ?? currentStatus;
    return await fulfill(route, SetReleaseChannelResponseSchema, create(SetReleaseChannelResponseSchema));
  });

  await page.route(GET_UPGRADE_STATUS_RPC_PATTERN, async (route) => {
    return await fulfill(route, GetUpgradeStatusResponseSchema, currentUpgradeStatus);
  });

  await page.route(TRIGGER_UPGRADE_RPC_PATTERN, async (route) => {
    const request = fromJsonString(TriggerUpgradeRequestSchema, route.request().postData() ?? "{}");
    triggerUpgradeRequests.push(request.targetVersion);
    const operation = onTriggerUpgrade?.(request.targetVersion);
    currentUpgradeStatus = buildUpgradeStatus({ operation });
    return await fulfill(route, TriggerUpgradeResponseSchema, create(TriggerUpgradeResponseSchema, { operation }));
  });

  return {
    releaseChannelRequests,
    triggerUpgradeRequests,
  };
}

async function fulfill<T extends Message>(route: Route, schema: GenMessage<T>, message: T) {
  return await route.fulfill({
    status: 200,
    contentType: "application/json",
    body: toJsonString(schema, message),
  });
}
