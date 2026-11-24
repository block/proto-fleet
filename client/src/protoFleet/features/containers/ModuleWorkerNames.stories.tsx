import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react";
import { HashboardCell } from "./HashboardCell";
import { InventoryCell } from "./InventoryCell";
import {
  EntityKind,
  HashboardSlotSchema,
  InventoryItemSchema,
} from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";

const meta = { title: "Proto Fleet/Containers/Module worker names" } satisfies Meta;
export default meta;
type Story = StoryObj<typeof meta>;

export const Strategies: Story = {
  render: () => (
    <table className="w-full text-left">
      <thead>
        <tr>
          <th className="p-3">Module</th>
          <th className="p-3">Worker name</th>
          <th className="p-3">Hashboard 1</th>
          <th className="p-3">Hashboard 2</th>
          <th className="p-3">Hashboard 3</th>
        </tr>
      </thead>
      <tbody>
        {[
          { summary: "MAC address", names: ["02:00:00:00:e2:45", "02:00:00:00:cb:d6", "02:00:00:00:6c:23"] },
          { summary: "", names: ["", "", ""] },
          { summary: "rack-one", names: ["rack-one", "rack-one", "rack-one"] },
          { summary: "Mixed", names: ["02:00:00:00:e2:45", "rack-one", ""] },
          { summary: "Mixed", names: ["rack-one", "rack-two", "rack-three"] },
        ].map(({ summary, names }, index) => (
          <tr key={index}>
            <td className="p-3">Module {index + 1}</td>
            <td className="p-3">
              <InventoryCell
                item={create(InventoryItemSchema, {
                  ref: { kind: EntityKind.MODULE, id: BigInt(index + 1) },
                  workerNameSummary: summary,
                })}
                column="workerName"
              />
            </td>
            {names.map((workerName, position) => (
              <td key={position} className="p-3">
                <HashboardCell
                  slot={create(HashboardSlotSchema, {
                    position: position + 1,
                    occupancy: "occupied",
                    board: { workerName },
                  })}
                  column="workerName"
                />
              </td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  ),
};
