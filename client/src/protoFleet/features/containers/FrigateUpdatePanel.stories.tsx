import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import type { Meta, StoryObj } from "@storybook/react";
import FrigateUpdatePanel from "./FrigateUpdatePanel";
import {
  FrigateAgentPhase,
  FrigateRolloutPhase,
  FrigateUpdateState,
  InstanceProgressSchema,
  InstanceStageState,
  UpdateJobSchema,
} from "@/protoFleet/api/generated/frigateupdate/v1/frigateupdate_pb";

const now = Date.now();
const ago = (seconds: number) => timestampFromDate(new Date(now - seconds * 1000));

const instance = (instanceId: string, overrides: Record<string, unknown> = {}) =>
  create(InstanceProgressSchema, {
    instanceId,
    stageState: InstanceStageState.STAGED,
    agentPhase: FrigateAgentPhase.STAGED,
    lastContact: ago(3),
    ...overrides,
  });

const job = (overrides: Record<string, unknown> = {}) =>
  create(UpdateJobSchema, {
    jobId: "11111111-1111-4111-8111-111111111111",
    state: FrigateUpdateState.RUNNING,
    rolloutPhase: FrigateRolloutPhase.INSTALLING,
    fromVersion: "1.3.0",
    targetVersion: "1.4.0",
    driverInstanceId: "frigate-a",
    createdAt: ago(600),
    instances: [
      instance("frigate-a", { agentPhase: FrigateAgentPhase.INSTALLED }),
      instance("frigate-b", { agentPhase: FrigateAgentPhase.STOPPED }),
    ],
    ...overrides,
  });

const earlier = [
  job({
    jobId: "22222222-2222-4222-8222-222222222222",
    state: FrigateUpdateState.SUCCEEDED,
    fromVersion: "1.2.0",
    targetVersion: "1.3.0",
    createdAt: ago(86400 * 30),
  }),
  job({
    jobId: "33333333-3333-4333-8333-333333333333",
    state: FrigateUpdateState.CANCELLED,
    fromVersion: "1.2.0",
    targetVersion: "1.2.1",
    createdAt: ago(86400 * 40),
  }),
];

const meta: Meta<typeof FrigateUpdatePanel> = {
  title: "Proto Fleet/Containers/Frigate Update",
  component: FrigateUpdatePanel,
  args: { loading: false, onChanged: () => undefined },
  decorators: [
    (Story) => (
      <div className="max-w-3xl p-6">
        <Story />
      </div>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof FrigateUpdatePanel>;

export const Running: Story = {
  args: { jobs: [job({ nextStep: "Wait for both controllers to restart." }), ...earlier] },
};

export const Staging: Story = {
  args: {
    jobs: [
      job({
        state: FrigateUpdateState.STAGING,
        rolloutPhase: FrigateRolloutPhase.UNSPECIFIED,
        cancellable: true,
        instances: [
          instance("frigate-a", {
            stageState: InstanceStageState.STAGING,
            agentPhase: FrigateAgentPhase.UNSPECIFIED,
            stagedBytes: 40n * 1024n * 1024n,
            sizeBytes: 96n * 1024n * 1024n,
          }),
          instance("frigate-b", {
            stageState: InstanceStageState.PENDING,
            agentPhase: FrigateAgentPhase.UNSPECIFIED,
          }),
        ],
      }),
    ],
  },
};

export const ServedByMaintenance: Story = {
  args: {
    jobs: [
      job({
        servedByMaintenance: true,
        nextStep: "No action needed. Frigate restarts when the install finishes.",
        instances: [
          instance("frigate-a", { agentPhase: FrigateAgentPhase.STOPPED }),
          instance("frigate-b", { agentPhase: FrigateAgentPhase.STAGED, lastContact: ago(95), contactStale: true }),
        ],
      }),
    ],
  },
};

export const FailedRetryable: Story = {
  args: {
    jobs: [
      job({
        state: FrigateUpdateState.FAILED,
        rolloutPhase: FrigateRolloutPhase.FAILED,
        errorCode: "UPDATE_MIGRATE_FAILED",
        errorMessage: "migration 42 failed on frigate-a",
        nextStep: "Fix the database, then retry the update.",
        retryable: true,
        disruptionStarted: true,
        instances: [
          instance("frigate-a", {
            agentPhase: FrigateAgentPhase.FAILED,
            errorCode: "UPDATE_MIGRATE_FAILED",
            errorMessage: "migration 42 failed",
          }),
          instance("frigate-b", { agentPhase: FrigateAgentPhase.STOPPED }),
        ],
      }),
      ...earlier,
    ],
  },
};

export const UnknownErrorCode: Story = {
  args: {
    jobs: [
      job({ state: FrigateUpdateState.FAILED, errorCode: "UPDATE_BRAND_NEW", errorMessage: "Something unexpected." }),
    ],
  },
};

export const NoControllerAnswering: Story = {
  args: {
    jobs: [
      job({
        unreachableSince: ago(240),
        instances: [instance("frigate-a", { contactStale: true, lastContact: ago(240) })],
      }),
    ],
  },
};
