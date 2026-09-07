import type { Meta, StoryObj } from "@storybook/react";
import { action } from "storybook/actions";

import BulkCloseModal from "./BulkCloseModal";

const meta = {
  title: "Proto Fleet/Maintenance/Bulk close",
  component: BulkCloseModal,
  args: {
    onDismiss: action("dismiss bulk close"),
    onSuccess: action("close tickets"),
  },
  parameters: {
    layout: "fullscreen",
    docs: {
      description: {
        component: "Review the shared resolution and notes applied when closing one or several maintenance tickets.",
      },
    },
  },
  tags: ["autodocs"],
} satisfies Meta<typeof BulkCloseModal>;

export default meta;
type Story = StoryObj<typeof meta>;

export const MultipleTickets: Story = {
  args: {
    ticketIds: ["1", "3", "7"],
  },
};

export const SingleTicket: Story = {
  args: {
    ticketIds: ["1"],
  },
};
