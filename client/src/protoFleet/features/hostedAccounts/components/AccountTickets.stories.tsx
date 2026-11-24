import { useState } from "react";
import { MemoryRouter } from "react-router-dom";
import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react";
import { expect, userEvent, within } from "storybook/test";

import { type AccountScope, AccountScopeContext } from "../AccountScope";
import AccountTickets from "./AccountTickets";
import { AccountRole, AccountSchema, AccountTicketDetailSchema } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import {
  TicketAssetSchema,
  TicketCategory,
  TicketStatus,
} from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";

const AccountTicketPreview = ({
  detail = false,
  closed = false,
  hostCreated = false,
  readOnly = false,
}: {
  detail?: boolean;
  closed?: boolean;
  hostCreated?: boolean;
  readOnly?: boolean;
}) => {
  const [scope] = useState<AccountScope>(() => {
    const account = create(AccountSchema, {
      accountId: "11111111-1111-4111-8111-111111111111",
      name: "Northstar Mining",
      depth: 1,
      role: AccountRole.ADMINISTRATOR,
      effectiveCapabilities: 31n,
      ticketingEnabled: true,
      ticketAccess: true,
    });
    const incident = create(AccountTicketDetailSchema, {
      ticket: {
        id: 7n,
        ticketNumber: "TK-0007",
        category: TicketCategory.ACCOUNT,
        description: "Several units stopped reporting this morning. Please investigate.",
        status: closed ? TicketStatus.COMPLETED : TicketStatus.OPEN,
        updatedAt: { seconds: 1793335200n, nanos: 123456789 },
        resolutionNote: closed ? "Connections restored and verified." : "",
      },
      canClose: !hostCreated && !closed,
      comments: [
        {
          id: 1n,
          ticketId: 7n,
          userName: "Host support",
          text: "We are checking the site connection.",
          createdAt: { seconds: 1793335200n },
        },
      ],
    });
    const assets = [
      create(TicketAssetSchema, {
        kind: TicketCategory.MODULE,
        id: 51n,
        label: "Module 51",
        model: "Model A",
        siteId: 1n,
      }),
      create(TicketAssetSchema, {
        kind: TicketCategory.HASHBOARD,
        id: 84n,
        label: "HB-84",
        moduleId: 51n,
        model: "Model A",
        siteId: 1n,
      }),
    ];
    const client = {
      listAccountTickets: async () => ({ tickets: [incident.ticket], nextBeforeId: 0n }),
      getAccountTicket: async () => ({ detail: incident }),
      listAccountTicketAssets: async ({ search }: { search: string }) => ({
        nextPageToken: "",
        assets: assets.filter((asset) => `${asset.label} ${asset.id}`.toLowerCase().includes(search.toLowerCase())),
      }),
      createAccountTicket: async ({
        description,
        component,
        targets,
      }: {
        description: string;
        component: string;
        targets: { id: bigint }[];
      }) => {
        incident.ticket!.description = description;
        incident.ticket!.component = component;
        incident.ticket!.targets = assets.filter((asset) => targets.some((target) => target.id === asset.id));
        return { detail: incident };
      },
      commentAccountTicket: async ({ text }: { text: string }) => {
        incident.comments.push({
          ...incident.comments[0],
          id: BigInt(incident.comments.length + 1),
          userName: "Client teammate",
          text,
        });
        return { detail: incident };
      },
      changeAccountTicketStatus: async ({ close, resolutionNote }: { close: boolean; resolutionNote: string }) => {
        incident.ticket!.status = close ? TicketStatus.COMPLETED : TicketStatus.OPEN;
        incident.ticket!.resolutionNote = close ? resolutionNote : "";
        incident.canClose = !close;
        return { detail: incident };
      },
    };
    return {
      account,
      client: client as unknown as AccountScope["client"],
      signal: new AbortController().signal,
      readOnly,
      refreshAccount: () => {},
      invalidateAccount: () => {},
    };
  });
  return (
    <MemoryRouter initialEntries={[`/accounts/${scope.account.accountId}/tickets${detail ? "?ticket=7" : ""}`]}>
      <AccountScopeContext.Provider value={scope}>
        <div className="min-h-screen bg-surface-base p-6 text-text-primary laptop:p-10">
          <AccountTickets />
        </div>
      </AccountScopeContext.Provider>
    </MemoryRouter>
  );
};
const meta = {
  title: "Proto Fleet/Clients/Tickets",
  component: AccountTickets,
  parameters: { layout: "fullscreen", withRouter: false },
} satisfies Meta<typeof AccountTickets>;
export default meta;
type Story = StoryObj<typeof meta>;
export const TicketList: Story = { render: () => <AccountTicketPreview /> };
export const NewTicket: Story = {
  render: () => <AccountTicketPreview />,
  play: async ({ canvasElement }) => {
    await userEvent.click(await within(canvasElement.ownerDocument.body).findByRole("button", { name: "New ticket" }));
    await expect(within(canvasElement.ownerDocument.body).getByRole("button", { name: "Component" })).toHaveTextContent(
      "Unknown",
    );
  },
};
export const ClientTicketDetail: Story = { render: () => <AccountTicketPreview detail /> };
export const HostTicketDetail: Story = { render: () => <AccountTicketPreview detail hostCreated /> };
export const ClosedTicket: Story = { render: () => <AccountTicketPreview detail closed /> };
export const ReadOnly: Story = { render: () => <AccountTicketPreview detail readOnly /> };
