import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react";
import { action } from "storybook/actions";

import {
  ReleaseInfoSchema,
  UpgradeOperationSchema,
  UpgradePhase,
} from "@/protoFleet/api/generated/instance/v1/updates_pb";
import UpgradeOperationModal from "@/protoFleet/features/settings/components/UpgradeOperationModal";

const release = (version = "v1.3.0", prerelease = false) =>
  create(ReleaseInfoSchema, {
    version,
    prerelease,
  });

const operation = (phase: UpgradePhase, message: string) =>
  create(UpgradeOperationSchema, {
    id: "operation-1",
    targetVersion: "v1.3.0",
    phase,
    message,
  });

const meta = {
  title: "Proto Fleet/Settings/UpgradeOperationModal",
  component: UpgradeOperationModal,
  parameters: {
    layout: "fullscreen",
  },
  args: {
    connectionLost: false,
    manualFallbackReady: false,
    onAcknowledge: action("acknowledge failure"),
    onDismiss: action("dismiss modal"),
    onReload: action("reload Fleet"),
    onUpgrade: (targetVersion: string) => {
      action("start upgrade")(targetVersion);
      return Promise.resolve();
    },
    onUseManualFallback: action("unlock manual install"),
    open: true,
    reconciling: false,
    release: release(),
    triggerError: null,
    triggering: false,
  },
} satisfies Meta<typeof UpgradeOperationModal>;

export default meta;
type Story = StoryObj<typeof meta>;

export const StableConfirmation: Story = {};

export const ReleaseCandidateConfirmation: Story = {
  args: {
    release: release("v1.3.0-rc.2", true),
  },
};

export const CheckingStatus: Story = {
  args: {
    reconciling: true,
    targetVersion: "v1.3.0",
    triggerError: "The upgrade request timed out before Fleet received a response.",
  },
};

export const ManualFallbackConfirmation: Story = {
  args: {
    connectionLost: true,
    manualFallbackReady: true,
    reconciling: true,
    targetVersion: "v1.3.0",
    triggerError: "The host updater is not reporting the tracked upgrade.",
  },
};

export const Preflight: Story = {
  args: {
    operation: operation(UpgradePhase.PREFLIGHT, "Validating the new stack"),
  },
};

export const RestartingServices: Story = {
  args: {
    connectionLost: true,
    operation: operation(UpgradePhase.ACTIVATING, "Restarting Fleet services"),
  },
};

export const FailedWithRecovery: Story = {
  args: {
    operation: create(UpgradeOperationSchema, {
      id: "operation-1",
      targetVersion: "v1.3.0",
      phase: UpgradePhase.FAILED,
      message: "Upgrade failed",
      error: "The replacement stack did not become ready.",
      hostLogPath: "/var/lib/proto-fleet-updater/logs/operation-1.log",
      recoveryCommand: "cd /opt/proto-fleet/deployment && ./run-fleet.sh --non-interactive --skip-build",
    }),
  },
};

export const Succeeded: Story = {
  args: {
    operation: operation(UpgradePhase.SUCCEEDED, "Fleet v1.3.0 is running"),
  },
};

export const TriggerError: Story = {
  args: {
    triggerError: "The host updater rejected the request. Review the host logs and try again.",
  },
};
