import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react";
import { action } from "storybook/actions";
import { expect, spyOn, userEvent, within } from "storybook/test";
import MaintenanceOptionsProvider from "../MaintenanceOptionsProvider";
import CreateTicketModal from "./CreateTicketModal";
import { maintenanceClient, sitesClient } from "@/protoFleet/api/clients";
import {
  CreateRepairTicketResponseSchema,
  ListAssigneesResponseSchema,
  ListTicketAssetOptionsResponseSchema,
  TicketCategory,
} from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";
import { ListSitesResponseSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { useFleetStore } from "@/protoFleet/store";

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
          "Create Miner, Infrastructure, or Other tickets with component defaults, affected equipment, assignment, and visibility.",
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
      description: "Hashboard not detected",
      siteId: "Denver",
    },
  },
};

const creationPreview = () => {
  const permissions = useFleetStore.getState().auth.permissions;
  useFleetStore.setState((state) => {
    state.auth.permissions = ["maintenance:read", "maintenance:manage", "miner:read"];
  });
  const mocks = [
    spyOn(sitesClient, "listSites").mockResolvedValue(
      create(ListSitesResponseSchema, { sites: [{ site: { id: 8n, name: "Denver" } }] }),
    ),
    spyOn(maintenanceClient, "listAssignees").mockResolvedValue(
      create(ListAssigneesResponseSchema, { assignees: [{ userId: 3n, username: "Alex", roleName: "Technician" }] }),
    ),
    spyOn(maintenanceClient, "listTicketAssetOptions").mockResolvedValue(
      create(ListTicketAssetOptionsResponseSchema, {
        assets: [
          { kind: TicketCategory.MINER, id: 12n, label: "M0012", model: "Antminer S21", siteId: 8n },
          { kind: TicketCategory.MODULE, id: 51n, label: "Module 51", model: "Proto module", siteId: 8n },
          {
            kind: TicketCategory.HASHBOARD,
            id: 84n,
            moduleId: 51n,
            label: "HB-84",
            model: "Proto hashboard",
            siteId: 8n,
          },
          { kind: TicketCategory.MODULE, id: 52n, label: "Module 52", model: "Proto module", siteId: 8n },
        ],
      }),
    ),
    spyOn(maintenanceClient, "createRepairTicket").mockResolvedValue(create(CreateRepairTicketResponseSchema)),
  ];
  return () => {
    mocks.forEach((mock) => mock.mockRestore());
    useFleetStore.setState((state) => {
      state.auth.permissions = permissions;
    });
  };
};

export const OtherTicket: Story = {
  beforeEach: creationPreview,
  play: async ({ canvasElement }) => {
    const body = within(canvasElement.ownerDocument.body);
    await userEvent.click(await body.findByRole("button", { name: "Category" }));
    await userEvent.click(body.getByText("Other", { exact: true }));
    await userEvent.type(body.getByLabelText("Description"), "Site access needs investigation.");
    await expect(body.getByRole("button", { name: "Component" })).toHaveTextContent("Unknown");
  },
};

export const MinerEquipment: Story = {
  beforeEach: creationPreview,
  play: async ({ canvasElement }) => {
    const body = within(canvasElement.ownerDocument.body);
    await userEvent.click(await body.findByRole("button", { name: "Select affected equipment" }));
    const picker = within(await body.findByRole("dialog", { name: "Select affected assets" }));
    const choices = await picker.findAllByRole("checkbox");
    await userEvent.click(choices[1]);
    await userEvent.click(choices[2]);
    await expect(choices[0]).toBeDisabled();
    await expect(choices[3]).toBeDisabled();
    await userEvent.click(picker.getByRole("button", { name: "Select assets" }));
    await userEvent.type(body.getByLabelText("Description"), "Module and hashboard stopped reporting.");
  },
};
