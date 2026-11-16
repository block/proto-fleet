import { useLayoutEffect } from "react";
import type { Meta, StoryObj } from "@storybook/react";
import { action } from "storybook/actions";
import MiningGroupsDialog from "./MiningGroupsDialog";
import { useFleetStore } from "@/protoFleet/store";

const meta = {
  title: "Proto Fleet/Containers/Mining groups",
  component: MiningGroupsDialog,
  args: { deviceIdentifier: "ct1-mode-demo", mode: "deactivate", onClose: action("close") },
  decorators: [
    (Story, context) => {
      useLayoutEffect(() => {
        const previousPermissions = useFleetStore.getState().auth.permissions;
        useFleetStore
          .getState()
          .auth.setPermissions(["miner:deactivate_mining_groups", "miner:start_mining", "miner:stop_mining"]);
        const previousFetch = window.fetch;
        let enabled = true;
        window.fetch = async (input, init) => {
          const url = input instanceof Request ? input.url : String(input);
          if (!url.includes("/mininggroups.v1.MiningGroupService/")) return previousFetch(input, init);
          if (url.endsWith("/Deactivate")) enabled = false;
          return new Response(
            JSON.stringify(
              url.endsWith("/GetState")
                ? {
                    supported: true,
                    enabled,
                    status: enabled ? "idle" : "complete",
                    observedAt: new Date().toISOString(),
                    groups: [{ id: "client-group", macAddresses: ["02:00:00:00:00:01"] }],
                    canReturnToDefault: context.parameters.canReturnToDefault ?? false,
                    deactivationBlockedReason: context.parameters.blockedReason ?? "",
                  }
                : { operationId: "story-deactivation" },
            ),
            { headers: { "Content-Type": "application/json" } },
          );
        };
        return () => {
          window.fetch = previousFetch;
          useFleetStore.getState().auth.setPermissions(previousPermissions);
        };
      }, [context.parameters.blockedReason, context.parameters.canReturnToDefault]);
      return <Story />;
    },
  ],
} satisfies Meta<typeof MiningGroupsDialog>;
export default meta;
type Story = StoryObj<typeof meta>;

export const ReadyToDisable: Story = {};
export const Allocated: Story = {
  parameters: { blockedReason: "Recall all allocated hashboards before disabling mining groups." },
};
export const PendingCleanup: Story = {
  parameters: {
    blockedReason: "Wait for pending pool changes and ownership cleanup to finish before disabling mining groups.",
  },
};
export const MoveHashboards: Story = {
  parameters: {
    blockedReason: "Move hashboards to the default group before disabling mining groups.",
    canReturnToDefault: true,
  },
};
export const RecoveryRequired: Story = {
  parameters: {
    blockedReason: "Wait for pending device changes or resolve uncertain writes before disabling mining groups.",
  },
};
