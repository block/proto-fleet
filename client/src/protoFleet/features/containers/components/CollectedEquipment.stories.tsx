import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react";
import { buildEquipmentOverview } from "../buildEquipmentOverview";
import ContainerOverviewLayout from "./ContainerOverviewLayout";
import { ContainerSchema } from "@/protoFleet/api/generated/containers/v1/containers_pb";

function CollectedEquipment({ state }: { state: "populated" | "unavailable" | "stale" }) {
  const container = create(
    ContainerSchema,
    state === "unavailable"
      ? {}
      : {
          fans: [
            {
              componentId: "dry-cooler-1",
              speedPercent: 65,
              powerW: 420,
              direction: "PLANT_COOLING_FAN_DIRECTION_FORWARD",
            },
            { componentId: "dry-cooler-2", speedPercent: 0, powerW: 0 },
            { componentId: "dry-cooler-3", alarmActive: true },
          ].map((fan) => ({ ...fan, stale: state === "stale" })),
          controls: {
            supported: true,
            stale: state === "stale",
            controls: [
              { name: "dry_cooler", enabled: true },
              { name: "dry_cooler_auto", enabled: true },
              { name: "tank_led_light", enabled: false },
              { name: "coolant_pump", enabled: true },
              { name: "air_conditioner_1", enabled: true },
              { name: "air_conditioner_2" },
            ],
            setpoints: [
              { name: "coolant_pump_speed_hz", value: 50, unit: "Hz" },
              { name: "dry_cooler_auto_temp_c", value: 32, unit: "°C" },
            ],
            mainCircuitBreakerSupported: true,
            mainCircuitBreakerClosed: true,
          },
        },
  );
  const equipment = buildEquipmentOverview(container);
  return (
    <ContainerOverviewLayout
      title="CT1 North"
      breadcrumb={[{ label: "North site" }, { label: "CT1 North" }]}
      kpis={[]}
      tanks={[]}
      fans={equipment.fans}
      controls={equipment.controls.length ? equipment.controls : undefined}
      plantControls={equipment.controlsNotice ? <p>{equipment.controlsNotice}</p> : undefined}
    />
  );
}

const meta = {
  title: "Proto Fleet/Containers/Collected equipment",
  component: CollectedEquipment,
  args: { state: "populated" },
} satisfies Meta<typeof CollectedEquipment>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Populated: Story = {};
export const Unavailable: Story = { args: { state: "unavailable" } };
export const Stale: Story = { args: { state: "stale" } };
