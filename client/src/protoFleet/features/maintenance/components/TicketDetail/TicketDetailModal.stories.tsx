import type { Meta, StoryObj } from "@storybook/react";
import { action } from "storybook/actions";

import TicketDetailModal from "./TicketDetailModal";

const ACTIVE_TICKET_IDS = ["1", "2", "3", "4", "5", "6", "7", "8", "9", "10"];
const COMPLETED_TICKET_IDS = ["101", "102", "103", "104", "105", "106", "107", "108", "109", "110"];

const meta = {
  title: "Proto Fleet/Maintenance/Ticket detail",
  component: TicketDetailModal,
  args: {
    onDismiss: action("dismiss ticket detail"),
  },
  parameters: {
    layout: "fullscreen",
    docs: {
      description: {
        component:
          "Compare ticket details across lifecycle states. Assignment, status, completion, comments, RMA, linked asset, and previous/next controls remain interactive.",
      },
    },
  },
  tags: ["autodocs"],
} satisfies Meta<typeof TicketDetailModal>;

export default meta;
type Story = StoryObj<typeof meta>;

export const OpenUrgentMiner: Story = {
  args: {
    ticketId: "1",
    ticketIds: ACTIVE_TICKET_IDS,
  },
};

export const RepairInProgress: Story = {
  args: {
    ticketId: "2",
    ticketIds: ACTIVE_TICKET_IDS,
  },
};

export const SentToVendor: Story = {
  args: {
    ticketId: "6",
    ticketIds: ACTIVE_TICKET_IDS,
  },
};

export const InfrastructureIssue: Story = {
  args: {
    ticketId: "7",
    ticketIds: ACTIVE_TICKET_IDS,
  },
};

export const CompletedRepair: Story = {
  args: {
    ticketId: "101",
    ticketIds: COMPLETED_TICKET_IDS,
  },
};
