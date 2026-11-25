import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react";
import { action } from "storybook/actions";
import TicketAssetPicker from "./TicketAssetPicker";
import { TicketAssetSchema, TicketCategory } from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";

const assets = [
  create(TicketAssetSchema, { kind: TicketCategory.MODULE, id: 1n, label: "Module 1" }),
  create(TicketAssetSchema, { kind: TicketCategory.MINER, id: 2n, label: "Miner 2" }),
  create(TicketAssetSchema, { kind: TicketCategory.HASHBOARD, id: 11n, moduleId: 1n, label: "Board 11" }),
  create(TicketAssetSchema, { kind: TicketCategory.HASHBOARD, id: 12n, moduleId: 1n, label: "Board 12" }),
];
const meta = {
  title: "Proto Fleet/Maintenance/Asset picker",
  component: TicketAssetPicker,
  parameters: { layout: "fullscreen" },
  args: {
    singleParent: true,
    onSelect: action("select assets"),
    onDismiss: action("dismiss picker"),
    load: async (search: string, _signal: AbortSignal, pageToken: string) => {
      const matching = assets.filter((asset) => asset.label.toLowerCase().includes(search.toLowerCase()));
      const offset = Number(pageToken);
      return {
        assets: matching.slice(offset, offset + 2),
        nextPageToken: offset + 2 < matching.length ? String(offset + 2) : "",
      };
    },
  },
} satisfies Meta<typeof TicketAssetPicker>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Paginated: Story = {};
