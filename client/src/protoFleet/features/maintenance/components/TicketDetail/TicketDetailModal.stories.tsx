import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react";
import { action } from "storybook/actions";
import { expect, spyOn, userEvent, waitFor, within } from "storybook/test";

import MaintenanceOptionsProvider from "../MaintenanceOptionsProvider";
import TicketDetailModal from "./TicketDetailModal";
import { maintenanceClient, sitesClient } from "@/protoFleet/api/clients";
import {
  CreateTicketCommentResponseSchema,
  DeleteTicketCommentResponseSchema,
  GetRepairTicketResponseSchema,
  ListAssigneesResponseSchema,
  RepairTicketSchema,
  TicketCategory,
  TicketStatus,
  UpdateRepairTicketResponseSchema,
} from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";
import { ListSitesResponseSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { useFleetStore } from "@/protoFleet/store";

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
  decorators: [
    (Story) => (
      <MaintenanceOptionsProvider>
        <Story />
      </MaintenanceOptionsProvider>
    ),
  ],
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

const assignmentPreview = (state: "empty" | "loading" | "error") => () => {
  const permissions = useFleetStore.getState().auth.permissions;
  useFleetStore.setState((store) => {
    store.auth.permissions = ["maintenance:read", "maintenance:manage"];
  });
  const ticket = create(RepairTicketSchema, {
    id: 255n,
    ticketNumber: "TK-0255",
    category: TicketCategory.INFRASTRUCTURE,
    status: TicketStatus.OPEN,
    component: "Electrical",
    diagnosis: "Breaker tripped",
    updatedAt: { seconds: 1_700_000_000n },
  });
  const assignees = spyOn(maintenanceClient, "listAssignees").mockResolvedValue(create(ListAssigneesResponseSchema));
  if (state === "loading") assignees.mockImplementation(() => new Promise(() => {}));
  if (state === "error") {
    assignees.mockRejectedValueOnce(new Error("Maintenance options could not be loaded."));
    assignees.mockResolvedValue(
      create(ListAssigneesResponseSchema, {
        assignees: [{ userId: 3n, username: "Sam", roleName: "Technician" }],
      }),
    );
  }
  const mocks = [
    assignees,
    spyOn(sitesClient, "listSites").mockResolvedValue(create(ListSitesResponseSchema)),
    spyOn(maintenanceClient, "getRepairTicket").mockImplementation(async () =>
      create(GetRepairTicketResponseSchema, { detail: { ticket } }),
    ),
    spyOn(maintenanceClient, "updateRepairTicket").mockImplementation(async (request) => {
      if (request.clearAssignee) {
        ticket.assigneeUserId = undefined;
        ticket.assigneeName = "";
      } else if (request.assigneeUserId !== undefined) {
        ticket.assigneeUserId = request.assigneeUserId;
        ticket.assigneeName = "Sam";
      }
      return create(UpdateRepairTicketResponseSchema, { ticket });
    }),
    spyOn(maintenanceClient, "createTicketComment").mockResolvedValue(create(CreateTicketCommentResponseSchema)),
    spyOn(maintenanceClient, "deleteTicketComment").mockResolvedValue(create(DeleteTicketCommentResponseSchema)),
  ];
  return () => {
    mocks.forEach((mock) => mock.mockRestore());
    useFleetStore.setState((store) => {
      store.auth.permissions = permissions;
    });
  };
};

const openAssignment: Story["play"] = async ({ canvasElement }) => {
  const body = within(canvasElement.ownerDocument.body);
  await waitFor(() => expect(body.getByRole("button", { name: "Update status" })).toBeVisible());
  const moreActions = body.queryByRole("button", { name: "More actions" });
  if (moreActions) await userEvent.click(moreActions);
  const assign = await body.findByRole("button", { name: "Assign" });
  await waitFor(() => expect(assign).toBeVisible());
  await userEvent.click(assign);
};

export const AssignmentEmpty: Story = {
  args: { ticketId: "255" },
  beforeEach: assignmentPreview("empty"),
  play: async (context) => {
    await openAssignment(context);
    await waitFor(() =>
      expect(within(context.canvasElement.ownerDocument.body).getByText("No assignees available.")).toBeVisible(),
    );
  },
};

export const AssignmentLoading: Story = {
  args: { ticketId: "255" },
  beforeEach: assignmentPreview("loading"),
  play: openAssignment,
};

export const AssignmentError: Story = {
  args: { ticketId: "255" },
  beforeEach: assignmentPreview("error"),
  play: async (context) => {
    await openAssignment(context);
    await waitFor(() =>
      expect(within(context.canvasElement.ownerDocument.body).getByRole("button", { name: "Retry" })).toBeVisible(),
    );
  },
};
