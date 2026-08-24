import { type ReactNode, useEffect } from "react";
import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react-vite";

import Updates from "./Updates";
import { instanceUpdateClient } from "@/protoFleet/api/clients";
import {
  type GetUpdateStatusResponse,
  GetUpdateStatusResponseSchema,
  GetUpgradeStatusResponseSchema,
  ReleaseChannel,
  ReleaseInfoSchema,
  SetReleaseChannelResponseSchema,
  TriggerUpgradeResponseSchema,
  type UpgradeOperation,
  UpgradeOperationSchema,
  UpgradePhase,
} from "@/protoFleet/api/generated/instance/v1/updates_pb";
import { useFleetStore } from "@/protoFleet/store";

type MutableClient<T> = { -readonly [K in keyof T]: T[K] };
type MessageOverrides<T> = Omit<Partial<T>, "$typeName" | "$unknown">;

interface UpdatesStoryState {
  operation?: UpgradeOperation;
  status: GetUpdateStatusResponse;
}

const mutableInstanceUpdateClient = instanceUpdateClient as MutableClient<typeof instanceUpdateClient>;

const release = (version = "v0.2.10", prerelease = false) =>
  create(ReleaseInfoSchema, {
    version,
    prerelease,
    releaseNotesUrl: `https://github.com/block/proto-fleet/releases/tag/${version}`,
  });

const status = (overrides: MessageOverrides<GetUpdateStatusResponse> = {}) =>
  create(GetUpdateStatusResponseSchema, {
    channel: ReleaseChannel.STABLE,
    currentVersion: "v0.2.9",
    installCommand:
      'bash <(curl -fsSL "https://github.com/block/proto-fleet/releases/download/v0.2.10/install.sh") v0.2.10',
    latestEligible: release(),
    oneClickAvailable: true,
    statusAvailable: true,
    updateAvailable: true,
    ...overrides,
  });

const operation = (phase: UpgradePhase, message: string) =>
  create(UpgradeOperationSchema, {
    id: "upgrade-1",
    targetVersion: "v0.2.10",
    phase,
    message,
  });

const updateAvailable: UpdatesStoryState = { status: status() };

const releaseCandidateAvailable: UpdatesStoryState = {
  status: status({
    channel: ReleaseChannel.STABLE_AND_RC,
    installCommand:
      'bash <(curl -fsSL "https://github.com/block/proto-fleet/releases/download/v0.2.10-rc.5/install.sh") v0.2.10-rc.5',
    latestEligible: release("v0.2.10-rc.5", true),
  }),
};

const oneClickUnavailable: UpdatesStoryState = {
  status: status({ oneClickAvailable: false }),
};

const upToDate: UpdatesStoryState = {
  status: status({
    currentVersion: "v0.2.10",
    installCommand: "",
    latestEligible: undefined,
    updateAvailable: false,
  }),
};

const statusUnavailable: UpdatesStoryState = {
  status: status({
    installCommand: "",
    latestEligible: undefined,
    statusAvailable: false,
    updateAvailable: false,
  }),
};

const updateInProgress: UpdatesStoryState = {
  operation: operation(UpgradePhase.DOWNLOADING, "Downloading release bundle"),
  status: status(),
};

const STORYBOOK_OPERATOR = "storybook-operator";

const UpdatesStoryHarness = ({ children, state }: { children: ReactNode; state: UpdatesStoryState }) => {
  const storyIsReady = useFleetStore(
    (current) =>
      current.auth.username === STORYBOOK_OPERATOR &&
      current.auth.isAuthenticated &&
      current.auth.permissions.includes("instance:update"),
  );

  useEffect(() => {
    let channel = state.status.channel;
    let activeOperation = state.operation;
    const previousAuth = useFleetStore.getState().auth;
    const originalGetUpdateStatus = mutableInstanceUpdateClient.getUpdateStatus;
    const originalSetReleaseChannel = mutableInstanceUpdateClient.setReleaseChannel;
    const originalGetUpgradeStatus = mutableInstanceUpdateClient.getUpgradeStatus;
    const originalTriggerUpgrade = mutableInstanceUpdateClient.triggerUpgrade;

    mutableInstanceUpdateClient.getUpdateStatus = async () =>
      create(GetUpdateStatusResponseSchema, { ...state.status, channel });
    mutableInstanceUpdateClient.setReleaseChannel = async (request) => {
      channel = request.channel ?? channel;
      return create(SetReleaseChannelResponseSchema);
    };
    mutableInstanceUpdateClient.getUpgradeStatus = async () =>
      create(GetUpgradeStatusResponseSchema, {
        executorAvailable: state.status.oneClickAvailable,
        operation: activeOperation,
      });
    mutableInstanceUpdateClient.triggerUpgrade = async (request) => {
      activeOperation = operation(UpgradePhase.PREFLIGHT, `Building and validating ${request.targetVersion}`);
      return create(TriggerUpgradeResponseSchema, { operation: activeOperation });
    };

    useFleetStore.setState((current) => ({
      auth: {
        ...current.auth,
        isAuthenticated: true,
        permissions: ["instance:update"],
        sessionExpiry: new Date(Date.now() + 60 * 60 * 1_000),
        sessionGeneration: current.auth.sessionGeneration + 1,
        username: STORYBOOK_OPERATOR,
      },
    }));

    return () => {
      mutableInstanceUpdateClient.getUpdateStatus = originalGetUpdateStatus;
      mutableInstanceUpdateClient.setReleaseChannel = originalSetReleaseChannel;
      mutableInstanceUpdateClient.getUpgradeStatus = originalGetUpgradeStatus;
      mutableInstanceUpdateClient.triggerUpgrade = originalTriggerUpgrade;
      useFleetStore.setState({ auth: previousAuth });
    };
  }, [state]);

  return storyIsReady ? <>{children}</> : null;
};

const meta = {
  title: "Proto Fleet/Settings/Updates",
  component: Updates,
  parameters: {
    layout: "fullscreen",
  },
  decorators: [
    (Story, context) => (
      <UpdatesStoryHarness state={context.parameters.updatesStoryState as UpdatesStoryState}>
        <div className="min-h-screen bg-surface-base p-10 phone:p-6">
          <div className="max-w-5xl">
            <Story />
          </div>
        </div>
      </UpdatesStoryHarness>
    ),
  ],
  tags: ["autodocs"],
} satisfies Meta<typeof Updates>;

export default meta;
type Story = StoryObj<typeof meta>;

export const UpdateAvailable: Story = {
  parameters: { updatesStoryState: updateAvailable },
};

export const ReleaseCandidateAvailable: Story = {
  parameters: { updatesStoryState: releaseCandidateAvailable },
};

export const OneClickUnavailable: Story = {
  parameters: { updatesStoryState: oneClickUnavailable },
};

export const UpToDate: Story = {
  parameters: { updatesStoryState: upToDate },
};

export const StatusUnavailable: Story = {
  parameters: { updatesStoryState: statusUnavailable },
};

export const UpdateInProgress: Story = {
  parameters: { updatesStoryState: updateInProgress },
};
