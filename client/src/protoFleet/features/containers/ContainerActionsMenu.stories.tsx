import { useLayoutEffect } from "react";
import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react";
import { CONTAINER_ACTIONS } from "./containerActions";
import ContainerCard from "./ContainerCard";
import { ContainerSchema, TankHealthSchema } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import BuildingList from "@/protoFleet/features/fleetManagement/components/BuildingList/BuildingList";
import { useFleetStore } from "@/protoFleet/store";

const container = create(ContainerSchema, {
  deviceIdentifier: "ct1-actions-demo",
  name: "Container 1",
  site: { id: 1n, label: "West site" },
  hashrateHs: 38e15,
  powerW: 920000,
  tanks: [0, 1, 2, 3, 6, 8].map((affected, index) =>
    create(TankHealthSchema, { position: index + 1, hashingCount: 16 - affected, attentionCount: affected }),
  ),
});

const meta = {
  title: "Proto Fleet/Containers/Actions",
  component: ContainerCard,
  args: { container },
  decorators: [
    (Story, context) => {
      useLayoutEffect(() => {
        const previous = useFleetStore.getState().auth.permissions;
        useFleetStore
          .getState()
          .auth.setPermissions([
            "miner:read",
            "miner:delete",
            ...CONTAINER_ACTIONS.map((action) => action.permission),
            "miner:firmware_update",
            "miner:reboot",
            "miner:update_pools",
            "pool:read",
            "miner:update_worker_names",
          ]);
        const previousFetch = window.fetch;
        let mining = context.parameters.containerMining !== false;
        // These examples never dispatch to a real controller.
        window.fetch = async (input, init) => {
          const url = input instanceof Request ? input.url : String(input);
          if (url.endsWith("/fleetmanagement.v1.FleetManagementService/DeleteMiners")) {
            return new Response(JSON.stringify({ deletedCount: 1 }), {
              headers: { "Content-Type": "application/json" },
            });
          }
          if (String(input).startsWith("/api-proxy/miners/ct1-actions-demo/")) {
            if (context.parameters.containerUnavailable) {
              return new Response(JSON.stringify({ error: "miner not found or cannot be proxied" }), {
                status: 404,
                headers: { "Content-Type": "application/json" },
              });
            }
            const body = init?.body ? JSON.parse(String(init.body)) : {};
            if (String(input).endsWith("/mining/stop")) mining = false;
            if (String(input).endsWith("/mining/start")) mining = true;
            return new Response(
              JSON.stringify(
                String(input).endsWith("/mining/status")
                  ? { mining }
                  : String(input).endsWith("/target-power")
                    ? { watts: body.watts ?? 920000, minimumWatts: 0, maximumWatts: 1200000 }
                    : {},
              ),
              {
                status: 200,
                headers: { "Content-Type": "application/json" },
              },
            );
          }
          return previousFetch(input, init);
        };
        return () => {
          useFleetStore.getState().auth.setPermissions(previous);
          window.fetch = previousFetch;
        };
      }, [context.parameters.containerMining, context.parameters.containerUnavailable]);
      return (
        <div className="p-6">
          <Story />
        </div>
      );
    },
  ],
} satisfies Meta<typeof ContainerCard>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Grid: Story = {
  render: (args) => (
    <div className="w-80 max-w-full">
      <ContainerCard {...args} />
    </div>
  ),
};
export const List: Story = {
  render: (args) => <BuildingList buildings={[]} containers={[args.container]} sites={[]} />,
};
export const SleepingGrid: Story = { ...Grid, parameters: { containerMining: false } };
export const SleepingList: Story = { ...List, parameters: { containerMining: false } };
export const UnavailableGrid: Story = { ...Grid, parameters: { containerUnavailable: true } };
export const UnavailableList: Story = { ...List, parameters: { containerUnavailable: true } };
export const Remote: Story = { ...UnavailableGrid };
