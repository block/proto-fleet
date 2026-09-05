import type { Meta, StoryObj } from "@storybook/react";
import { action } from "storybook/actions";

import CreateTicketModal from "./CreateTicketModal";

const meta = {
  title: "Proto Fleet/Maintenance/Create ticket",
  component: CreateTicketModal,
  args: {
    onDismiss: action("dismiss create ticket"),
    onSuccess: action("create ticket"),
  },
  parameters: {
    layout: "fullscreen",
    docs: {
      description: {
        component:
          "Review miner and infrastructure ticket creation, including category and component selection, miner lookup, assignment, and urgency.",
      },
    },
  },
  tags: ["autodocs"],
} satisfies Meta<typeof CreateTicketModal>;

export default meta;
type Story = StoryObj<typeof meta>;

export const EmptyForm: Story = {};

export const FromMinerAlert: Story = {
  args: {
    prefill: {
      alertId: "alert-hashboard-m0012",
      minerIdentifier: "M0012",
      component: "Hashboard",
      diagnosis: "Hashboard not detected",
      siteId: "Denver",
    },
  },
};
