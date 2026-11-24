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
          if (url.includes("/containers.v1.ContainerService/")) {
            if (context.parameters.containerUnavailable) {
              return new Response(JSON.stringify({ code: "unavailable", message: "Controller unavailable" }), {
                status: 503,
                headers: { "Content-Type": "application/json" },
              });
            }
            const body =
              input instanceof Request ? await input.clone().json() : init?.body ? JSON.parse(String(init.body)) : {};
            if (url.endsWith("/SetMining")) mining = body.mining;
            const result = url.endsWith("/GetMiningStatus")
              ? { mining }
              : url.endsWith("/GetPowerTarget")
                ? { watts: 3000, minimumWatts: 1000, maximumWatts: 5000 }
                : url.endsWith("/GetEquipmentCommandResult")
                  ? { result: { status: "finished", successCount: 1 } }
                  : { batchIdentifier: "story-command" };
            return new Response(JSON.stringify(result), { headers: { "Content-Type": "application/json" } });
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
