import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react";
import { HashboardCell } from "./HashboardCell";
import { InventoryCell } from "./InventoryCell";
import {
  EntityKind,
  HashboardSlotSchema,
  InventoryItemSchema,
} from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";

const meta = { title: "ProtoFleet/Containers/Collected metrics" } satisfies Meta;
export default meta;
type Story = StoryObj<typeof meta>;

/** Saved efficiency intentionally differs from power/hashrate to detect recomputation. */
export const SavedAndMissing: Story = {
  render: () => {
    const tank = create(InventoryItemSchema, {
      ref: { kind: EntityKind.TANK, id: 1n },
      name: "Tank 1",
      hashrateThs: 100,
      powerKw: 1.5,
      efficiencyJth: 10,
      minTemperatureC: 60,
      maxTemperatureC: 70,
      coverage: {
        hashrate: { reporting: 2, expected: 2, topologyComplete: true },
        power: { reporting: 1, expected: 1, topologyComplete: true },
        temperature: { reporting: 2, expected: 2, topologyComplete: true },
      },
    });
    const partial = create(InventoryItemSchema, {
      ref: { kind: EntityKind.TANK, id: 2n },
      name: "Tank 2",
      hashrateThs: 100,
      powerKw: 1.5,
      minTemperatureC: 60,
      maxTemperatureC: 70,
      efficiencyJth: undefined,
      coverage: {
        hashrate: { reporting: 1, expected: 2, topologyComplete: true },
        temperature: { reporting: 1, expected: 2, topologyComplete: true },
        power: { reporting: 1, expected: 1, topologyComplete: true },
      },
    });
    const zero = create(HashboardSlotSchema, {
      position: 1,
      occupancy: "occupied",
      board: { hashrateHs: 100e12, powerW: 0, efficiencyJh: 0 },
    });
    const missing = create(HashboardSlotSchema, { position: 2, occupancy: "occupied", board: {} });
    return (
      <table className="m-6 text-left [&_td]:p-4 [&_th]:p-4">
        <thead>
          <tr>
            <th>Equipment</th>
            <th>Hashrate</th>
            <th>Power</th>
            <th>Efficiency</th>
            <th>Temperature</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <th>Tank 1</th>
            {["hashrate", "power", "efficiency", "temperature"].map((column) => (
              <td key={column}>
                <InventoryCell item={tank} column={column} />
              </td>
            ))}
          </tr>
          <tr>
            <th>Tank 2</th>
            {["hashrate", "power", "efficiency", "temperature"].map((column) => (
              <td key={column}>
                <InventoryCell item={partial} column={column} />
              </td>
            ))}
          </tr>
          <tr>
            <th>Board with measured zero</th>
            {["hashrate", "power", "efficiency", "temperature"].map((column) => (
              <td key={column}>
                <HashboardCell slot={zero} column={column} />
              </td>
            ))}
          </tr>
          <tr>
            <th>Board without readings</th>
            {["hashrate", "power", "efficiency", "temperature"].map((column) => (
              <td key={column}>
                <HashboardCell slot={missing} column={column} />
              </td>
            ))}
          </tr>
        </tbody>
      </table>
    );
  },
};
