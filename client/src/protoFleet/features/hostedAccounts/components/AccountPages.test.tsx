import type { ReactNode } from "react";
import { MemoryRouter, useLocation } from "react-router-dom";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";

import { type AccountScope, AccountScopeContext } from "../AccountScope";
import { HostedAccountsContext } from "../HostedAccountsContext";
import AccountAccess from "./AccountAccess";
import AccountChildren from "./AccountChildren";
import AccountTeam from "./AccountTeam";
import { ClientPermissions } from "./ClientPermissions";
import {
  AccountAssetSchema,
  AccountRole,
  AccountSchema,
  InvitationSchema,
  ListAccountAssetsResponseSchema,
  MemberSchema,
  MiningAssetKind,
} from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import { useFleetStore } from "@/protoFleet/store";
import { pushToast, STATUSES } from "@/shared/features/toaster";

vi.mock("@/shared/features/toaster", () => ({
  pushToast: vi.fn(),
  STATUSES: { success: "success" },
}));

const initialAuth = useFleetStore.getState().auth;
const accountId = "11111111-1111-4111-8111-111111111111";
const CurrentLocation = () => {
  const location = useLocation();
  return <output data-testid="location">{location.pathname + location.search}</output>;
};
function scope(mask = 31n) {
  const client = {
    createAccount: vi.fn().mockResolvedValue({}),
    configureAccount: vi.fn().mockResolvedValue({}),
    listChildren: vi.fn().mockResolvedValue({
      accounts: [
        create(AccountSchema, {
          ticketingEnabled: true,
          accountId: "77777777-7777-4777-8777-777777777777",
          name: "Child",
          depth: 2,
          capabilityCeiling: 31n,
        }),
      ],
    }),
    listMembers: vi.fn().mockResolvedValue({
      members: [
        create(MemberSchema, {
          userId: "66666666-6666-4666-8666-666666666666",
          username: "payout_ops",
          role: AccountRole.POOL_OPERATOR,
        }),
      ],
    }),
    listInvitations: vi.fn().mockResolvedValue({ invitations: [], nextPageToken: "" }),
    createInvitation: vi.fn().mockResolvedValue({ token: "K7MP-4X9R" }),
    revokeMember: vi.fn().mockResolvedValue({}),
    revokeInvitation: vi.fn().mockResolvedValue({}),
    listAccountKeys: vi.fn().mockResolvedValue({ keys: [], nextPageToken: "" }),
    listAncestorGrants: vi.fn().mockResolvedValue({ grants: [] }),
    createAccountKey: vi.fn().mockResolvedValue({ secret: "one-time-test-secret" }),
    renameAccount: vi.fn().mockResolvedValue({}),
  };
  const value: AccountScope = {
    account: create(AccountSchema, {
      ticketingEnabled: true,
      accountId,
      parentAccountId: "55555555-5555-4555-8555-555555555555",
      name: "Client A",
      depth: 1,
      role: mask === 1n ? AccountRole.VIEWER : AccountRole.ADMINISTRATOR,
      effectiveCapabilities: mask,
      capabilityCeiling: 31n,
    }),
    client: client as unknown as AccountScope["client"],
    signal: new AbortController().signal,
    refreshAccount: vi.fn(),
    invalidateAccount: vi.fn(),
  };
  return { value, client };
}
beforeEach(() => {
  vi.mocked(pushToast).mockClear();
  // JSDOM has no layout; runtime popovers require a visible trigger rectangle.
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(20, 20, 240, 40));
});
afterEach(() => {
  vi.unstubAllGlobals();
  useFleetStore.setState({ auth: initialAuth });
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
const mount = (element: ReactNode, value: AccountScope) =>
  render(
    <MemoryRouter>
      <AccountScopeContext.Provider value={value}>{element}</AccountScopeContext.Provider>
    </MemoryRouter>,
  );

describe("hosted account pages", () => {
  it.each([
    { page: <AccountAccess />, controls: ["Create API key", "Grant host or parent access"] },
    { page: <AccountTeam />, controls: ["Invite team member"] },
    { page: <AccountChildren />, controls: ["Create subclient"] },
  ])("keeps administrator mutation controls visible and disabled in preview: $controls", async ({ page, controls }) => {
    const { value } = scope();
    mount(page, { ...value, readOnly: true });
    for (const name of controls) expect(await screen.findByRole("button", { name })).toBeDisabled();
  });
  it("shows members and unaccepted invitation statuses with applicable actions", async () => {
    const { value, client } = scope();
    const user = userEvent.setup();
    const now = BigInt(Math.floor(Date.now() / 1000));
    const pending = create(InvitationSchema, {
      invitationId: "pending",
      role: AccountRole.VIEWER,
      available: true,
      expiresAt: { seconds: now + 3600n },
    });
    const history = [
      create(InvitationSchema, { invitationId: "revoked", revokedAt: { seconds: now - 30n } }),
      create(InvitationSchema, { invitationId: "expired", available: true, expiresAt: { seconds: now - 30n } }),
      create(InvitationSchema, { invitationId: "unavailable" }),
    ];
    client.listInvitations
      .mockResolvedValueOnce({ invitations: [pending, ...history], nextPageToken: "" })
      .mockResolvedValue({
        invitations: [
          create(InvitationSchema, {
            invitationId: pending.invitationId,
            role: pending.role,
            expiresAt: pending.expiresAt,
            revokedAt: { seconds: now },
          }),
          ...history,
        ],
        nextPageToken: "",
      });
    mount(<AccountTeam />, value);
    const pendingRow = (await screen.findByText("Pending")).closest("tr")!;
    expect(screen.getAllByRole("table")).toHaveLength(1);
    expect(screen.getByRole("columnheader", { name: "Status" })).toBeInTheDocument();
    expect(screen.queryByRole("navigation", { name: "Account team sections" })).not.toBeInTheDocument();
    const memberRow = screen.getByText("payout_ops").closest("tr")!;
    expect(within(memberRow).getByText("Active")).toBeInTheDocument();
    expect(screen.queryByText("Accepted")).not.toBeInTheDocument();
    expect(screen.getAllByText("Invitation", { exact: true })).toHaveLength(4);
    for (const status of ["Revoked", "Expired", "Unavailable"]) {
      const row = screen.getByText(status).closest("tr")!;
      expect(within(row).queryByRole("button", { name: "Row actions" })).not.toBeInTheDocument();
    }
    await user.click(within(memberRow).getByRole("button", { name: "Row actions" }));
    expect(await screen.findByRole("button", { name: "Change role" })).toBeInTheDocument();
    expect(within(memberRow).queryByRole("button", { name: "Revoke invitation" })).not.toBeInTheDocument();
    await user.keyboard("{Escape}");
    expect(within(pendingRow).queryByRole("button", { name: "Change role" })).not.toBeInTheDocument();
    await user.click(within(pendingRow).getByRole("button", { name: "Revoke invitation" }));
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Revoke invitation" }));
    await waitFor(() => expect(client.revokeInvitation).toHaveBeenCalledWith({ accountId, invitationId: "pending" }));
    await waitFor(() => expect(screen.getAllByText("Revoked")).toHaveLength(2));
    expect(screen.queryByText("Pending")).not.toBeInTheDocument();
    expect(screen.getByText("payout_ops")).toBeInTheDocument();
  });
  it("pages through invitation history without duplicating active members on later pages", async () => {
    const { value, client } = scope();
    const user = userEvent.setup();
    client.listInvitations.mockImplementation(async ({ pageToken }: { pageToken: string }) =>
      pageToken
        ? {
            invitations: [create(InvitationSchema, { invitationId: "old", revokedAt: { seconds: 1n } })],
            nextPageToken: "",
          }
        : {
            invitations: [create(InvitationSchema, { invitationId: "new", available: true })],
            nextPageToken: "next-invitations",
          },
    );
    mount(<AccountTeam />, value);
    await screen.findByText("Pending");
    expect(screen.getByText("payout_ops")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Next" }));
    await screen.findByText("Revoked");
    expect(screen.queryByText("payout_ops")).not.toBeInTheDocument();
    expect(client.listInvitations).toHaveBeenLastCalledWith(
      { accountId, pageSize: 25, pageToken: "next-invitations" },
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
    expect(screen.getByRole("button", { name: "Next" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Previous" }));
    await screen.findByText("payout_ops");
    expect(screen.getByText("Pending")).toBeInTheDocument();
  });
  it("shows a portable invitation code once and keeps a fragment link as a convenience", async () => {
    const user = userEvent.setup(),
      { value, client } = scope();
    mount(<AccountTeam />, value);
    await user.click(await screen.findByRole("button", { name: "Invite team member" }));
    await user.click(screen.getByRole("button", { name: "Expires after" }));
    const expiryOptions = within(screen.getByRole("listbox", { name: "Expires after options" }));
    expect(expiryOptions.getAllByRole("option").map((option) => option.textContent)).toEqual(["1 hour", "24 hours"]);
    await user.click(expiryOptions.getByRole("option", { name: "24 hours" }));
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Create invitation" }));
    const secret = await screen.findByLabelText("Invitation code");
    expect(secret).toHaveValue("K7MP-4X9R");
    vi.stubGlobal("isSecureContext", true);
    const copy = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue();
    await user.click(screen.getByRole("button", { name: "Copy invitation code" }));
    expect(copy).toHaveBeenLastCalledWith("K7MP-4X9R");
    await user.click(screen.getByRole("button", { name: "Copy invitation link" }));
    expect(copy).toHaveBeenLastCalledWith(`${window.location.origin}/account-invitation#token=K7MP-4X9R`);
    expect(client.createInvitation).toHaveBeenCalledWith({
      accountId,
      role: AccountRole.VIEWER,
      expiresInSeconds: 86400,
    });
    await user.click(screen.getByRole("button", { name: "Done" }));
    expect(screen.queryByLabelText("Invitation code")).not.toBeInTheDocument();
    expect(screen.queryByLabelText(/email/i)).not.toBeInTheDocument();
  });
  it.each([
    { team: "an empty team", members: [], role: AccountRole.ADMINISTRATOR, label: "Account administrator" },
    { team: "a staffed team", members: undefined, role: AccountRole.VIEWER, label: "Viewer" },
  ])("defaults deep-linked invitations for $team", async ({ members, role, label }) => {
    const user = userEvent.setup(),
      { value, client } = scope();
    if (members) client.listMembers.mockResolvedValue({ members });
    render(
      <MemoryRouter initialEntries={["/team?invite=1"]}>
        <AccountScopeContext.Provider value={value}>
          <AccountTeam />
        </AccountScopeContext.Provider>
      </MemoryRouter>,
    );
    const dialog = await screen.findByRole("dialog");
    await waitFor(() => expect(client.listInvitations).toHaveBeenCalled());
    if (!members) await screen.findByText("payout_ops");
    expect(await within(dialog).findByText(label)).toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: "Create invitation" }));
    await waitFor(() =>
      expect(client.createInvitation).toHaveBeenCalledWith({ accountId, role, expiresInSeconds: 86400 }),
    );
  });
  it("defaults another invitation to Viewer while the first invitation is pending", async () => {
    const user = userEvent.setup(),
      { value, client } = scope();
    client.listMembers.mockResolvedValue({ members: [] });
    client.listInvitations.mockResolvedValue({
      invitations: [create(InvitationSchema, { invitationId: "pending", available: true })],
      nextPageToken: "",
    });
    mount(<AccountTeam />, value);
    await user.click(await screen.findByRole("button", { name: "Invite team member" }));
    const dialog = screen.getByRole("dialog", { name: "Create invitation" });
    expect(within(dialog).getByRole("button", { name: "Role" })).toHaveTextContent("Viewer");
    await user.click(within(dialog).getByRole("button", { name: "Create invitation" }));
    await waitFor(() =>
      expect(client.createInvitation).toHaveBeenCalledWith({
        accountId,
        role: AccountRole.VIEWER,
        expiresInSeconds: 86400,
      }),
    );
  });
  it("waits for the team query before opening a deep-linked invitation", async () => {
    const user = userEvent.setup(),
      { value, client } = scope();
    let finish!: () => void;
    client.listMembers.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = () => resolve({ members: [] });
        }),
    );
    render(
      <MemoryRouter initialEntries={["/team?invite=1"]}>
        <AccountScopeContext.Provider value={value}>
          <AccountTeam />
        </AccountScopeContext.Provider>
      </MemoryRouter>,
    );
    await waitFor(() => expect(client.listMembers).toHaveBeenCalledOnce());
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Create invitation" })).not.toBeInTheDocument();
    expect(client.createInvitation).not.toHaveBeenCalled();
    await act(async () => finish());
    const dialog = await screen.findByRole("dialog", { name: "Create invitation" });
    expect(within(dialog).getByRole("button", { name: "Role" })).toHaveTextContent("Account administrator");
    await user.click(within(dialog).getByRole("button", { name: "Create invitation" }));
    await waitFor(() =>
      expect(client.createInvitation).toHaveBeenCalledWith({
        accountId,
        role: AccountRole.ADMINISTRATOR,
        expiresInSeconds: 86400,
      }),
    );
  });
  it("copies invitation codes and optional links on plain HTTP without a secure clipboard API", async () => {
    const user = userEvent.setup(),
      { value } = scope();
    vi.stubGlobal("isSecureContext", false);
    const descriptor = Object.getOwnPropertyDescriptor(document, "execCommand");
    const copied: string[] = [];
    const copy = vi.fn(() => {
      copied.push((document.activeElement as HTMLTextAreaElement).value);
      return true;
    });
    Object.defineProperty(document, "execCommand", { configurable: true, value: copy });
    try {
      mount(<AccountTeam />, value);
      await user.click(await screen.findByRole("button", { name: "Invite team member" }));
      await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Create invitation" }));
      await screen.findByLabelText("Invitation code");
      await user.click(screen.getByRole("button", { name: "Copy invitation code" }));
      expect(await screen.findByRole("button", { name: "Invitation code copied" })).toBeInTheDocument();
      await user.click(screen.getByRole("button", { name: "Copy invitation link" }));
      await screen.findByRole("button", { name: "Invitation link copied" });
      expect(copied).toEqual(["K7MP-4X9R", `${window.location.origin}/account-invitation#token=K7MP-4X9R`]);
      expect(copy).toHaveBeenCalledWith("copy");
      expect(document.querySelector("textarea")).toBeNull();
    } finally {
      if (descriptor) Object.defineProperty(document, "execCommand", descriptor);
      else Reflect.deleteProperty(document, "execCommand");
    }
  });
  it("keeps the code available for manual copying when both copy buttons fail", async () => {
    const user = userEvent.setup(),
      { value } = scope();
    vi.stubGlobal("isSecureContext", false);
    const descriptor = Object.getOwnPropertyDescriptor(document, "execCommand");
    Object.defineProperty(document, "execCommand", { configurable: true, value: vi.fn(() => false) });
    try {
      mount(<AccountTeam />, value);
      await user.click(await screen.findByRole("button", { name: "Invite team member" }));
      await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Create invitation" }));
      await screen.findByLabelText("Invitation code");
      await user.click(screen.getByRole("button", { name: "Copy invitation code" }));
      await screen.findByText("Couldn’t copy. Select and copy the value above.");
      await user.click(screen.getByRole("button", { name: "Copy invitation link" }));
      await screen.findByText("Couldn’t copy the link. Share the code instead.");
      expect(screen.getByLabelText("Invitation code")).toHaveValue("K7MP-4X9R");
      expect(document.querySelector("textarea")).toBeNull();
    } finally {
      if (descriptor) Object.defineProperty(document, "execCommand", descriptor);
      else Reflect.deleteProperty(document, "execCommand");
    }
  });
  it("removes only the selected account membership and leaves login administration absent", async () => {
    const user = userEvent.setup(),
      { value, client } = scope();
    mount(<AccountTeam />, value);
    const member = await screen.findByText("payout_ops");
    const row = member.closest("tr")!;
    await user.click(within(row).getByRole("button", { name: "Row actions" }));
    await user.click(await screen.findByRole("button", { name: "Remove membership" }));
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Remove membership" }));
    await waitFor(() =>
      expect(client.revokeMember).toHaveBeenCalledWith({ accountId, userId: "66666666-6666-4666-8666-666666666666" }),
    );
    expect(screen.queryByText("Reset Password")).not.toBeInTheDocument();
    expect(screen.queryByText("Deactivate")).not.toBeInTheDocument();
  });
  it("renames the account from Access and refreshes its displayed identity", async () => {
    const user = userEvent.setup(),
      { value, client } = scope();
    mount(<AccountAccess />, value);
    const details = screen.getByRole("region", { name: "Account details" });
    expect(within(details).getByText("Client A")).toBeInTheDocument();
    expect(within(details).getByText("Account administrator")).toBeInTheDocument();
    expect(within(details).queryByText("Active")).not.toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Your permissions" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Host and parent access" })).toBeInTheDocument();
    const permissions = within(screen.getByRole("region", { name: "Your permissions" }));
    expect(permissions.getByText("Allocate and recall assets")).toBeInTheDocument();
    expect(permissions.getByText("Change pools")).toBeInTheDocument();
    await user.click(within(details).getByRole("button", { name: "Rename account" }));
    const dialog = screen.getByRole("dialog", { name: "Rename account" });
    await user.clear(within(dialog).getByLabelText("Account name"));
    await user.type(within(dialog).getByLabelText("Account name"), "Renamed client");
    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(client.renameAccount).toHaveBeenCalledWith({ accountId, name: "Renamed client" }));
    expect(value.refreshAccount).toHaveBeenCalled();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
  it("shows suspension and the viewer's permissions without a rename control", async () => {
    const { value } = scope(1n);
    value.account.suspended = true;
    mount(<AccountAccess />, value);
    const details = screen.getByRole("region", { name: "Account details" });
    expect(within(details).getByText("Suspended")).toBeInTheDocument();
    expect(within(details).getByText("Viewer")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Rename account" })).not.toBeInTheDocument();
  });
  it("creates an account-only key by default and discards its secret on dismiss", async () => {
    const user = userEvent.setup(),
      { value, client } = scope(1n);
    mount(<AccountAccess />, value);
    await user.click(await screen.findByRole("button", { name: "Create API key" }));
    const dialog = within(screen.getByRole("dialog", { name: "Create API key" }));
    expect(dialog.getByRole("checkbox", { name: "View account data" })).toBeChecked();
    expect(dialog.getByRole("checkbox", { name: "View account data" })).toBeDisabled();
    expect(dialog.getByRole("checkbox", { name: "Manage pool configurations" })).toBeDisabled();
    expect(dialog.queryByRole("checkbox", { name: "Change pools" })).not.toBeInTheDocument();
    await user.type(screen.getByLabelText("Key name"), "Pool reader");
    await user.click(screen.getByRole("button", { name: "Create key" }));
    expect(await screen.findByLabelText("Secret")).toHaveValue("one-time-test-secret");
    expect(client.createAccountKey).toHaveBeenCalledWith(
      expect.objectContaining({
        accountId,
        name: "Pool reader",
        capabilities: 1n,
        descendantAccountIds: [],
        includeFutureDescendants: false,
      }),
    );
    await user.click(screen.getByRole("button", { name: "Done" }));
    expect(screen.queryByLabelText("Secret")).not.toBeInTheDocument();
  });

  it("creates a saved-pool management key without session-only profile selection", async () => {
    const user = userEvent.setup(),
      { value, client } = scope(7n);
    mount(<AccountAccess />, value);
    await user.click(await screen.findByRole("button", { name: "Create API key" }));
    const dialog = within(screen.getByRole("dialog", { name: "Create API key" }));
    expect(dialog.queryByRole("checkbox", { name: "Change pools" })).not.toBeInTheDocument();
    await user.click(dialog.getByRole("checkbox", { name: "Manage pool configurations" }));
    await user.type(dialog.getByLabelText("Key name"), "Pool automation");
    await user.click(dialog.getByRole("button", { name: "Create key" }));
    await waitFor(() =>
      expect(client.createAccountKey).toHaveBeenCalledWith(
        expect.objectContaining({ accountId, name: "Pool automation", capabilities: 3n }),
      ),
    );
  });

  it("identifies granted host access without presenting it as a membership role", async () => {
    const user = userEvent.setup(),
      { value } = scope();
    value.account.role = AccountRole.UNSPECIFIED;
    const host = create(AccountSchema, {
      ticketingEnabled: true,
      accountId: value.account.parentAccountId,
      name: "North Hosting",
    });
    mount(
      <HostedAccountsContext.Provider
        value={{ accounts: [host, value.account], hostAccess: true, loading: false, error: null, refresh: vi.fn() }}
      >
        <AccountAccess />
      </HostedAccountsContext.Provider>,
      value,
    );
    expect(
      within(screen.getByRole("region", { name: "Account details" })).getByText("Host and parent access"),
    ).toBeInTheDocument();
    await user.click(await screen.findByRole("button", { name: "Grant host or parent access" }));
    const dialog = within(screen.getByRole("dialog", { name: "Host and parent access" }));
    expect(dialog.getByRole("button", { name: "Host or parent account" })).toHaveTextContent("North Hosting");
    expect(dialog.getByText("Create, revise and archive saved pool configurations.")).toBeInTheDocument();
    expect(dialog.getByText("Apply pool configurations to equipment allocated to this account.")).toBeInTheDocument();
    expect(dialog.getByText("Allocate assets to subclients and recall them to this account.")).toBeInTheDocument();
  });

  it.each([false, true])("keeps subclient allocation on its existing route (suspended=%s)", async (suspended) => {
    const { value, client } = scope(16n);
    const child = create(AccountSchema, {
      accountId: "77777777-7777-4777-8777-777777777777",
      name: "Child",
      depth: 2,
      suspended,
      capabilityCeiling: 31n,
    });
    client.listChildren.mockResolvedValue({ accounts: [child] });
    const listAccountAssets = vi.fn().mockResolvedValue({
      assets: [],
      nextPageToken: "",
      canManageAssignments: true,
      children: [child],
    });
    render(
      <MemoryRouter initialEntries={[`/accounts/${accountId}/children?assets=${child.accountId}`]}>
        <AccountScopeContext.Provider
          value={{ ...value, client: { ...client, listAccountAssets } as unknown as AccountScope["client"] }}
        >
          <CurrentLocation />
          <AccountChildren />
        </AccountScopeContext.Provider>
      </MemoryRouter>,
    );
    await screen.findByText("Child");
    await waitFor(() => expect(listAccountAssets).toHaveBeenCalled());
    expect(screen.queryAllByRole("button", { name: "Allocate assets" })).toHaveLength(suspended ? 0 : 1);
    expect(screen.queryByRole("dialog", { name: "Select assets" })).not.toBeInTheDocument();
    if (!suspended) {
      await userEvent.setup().click(screen.getByRole("button", { name: "Allocate assets" }));
      expect(screen.getByTestId("location")).toHaveTextContent(
        `/accounts/${accountId}/children?assets=${child.accountId}&action=assign`,
      );
      await waitFor(() =>
        expect(listAccountAssets).toHaveBeenLastCalledWith(
          expect.objectContaining({ ownerAccountId: accountId }),
          expect.anything(),
        ),
      );
      expect(screen.queryByRole("dialog", { name: "Select assets" })).not.toBeInTheDocument();
      expect(screen.queryByRole("button", { name: "Allocate assets" })).not.toBeInTheDocument();
    }
  });

  it("shows the assignment confirmation in the child inventory it returns to", async () => {
    const { value, client } = scope();
    const user = userEvent.setup();
    const childId = "77777777-7777-4777-8777-777777777777";
    const listAccountAssets = vi.fn().mockImplementation(({ ownerAccountId }) =>
      Promise.resolve(
        create(ListAccountAssetsResponseSchema, {
          assets: [
            create(AccountAssetSchema, {
              kind: MiningAssetKind.MINER,
              id: 1n,
              revision: 1n,
              name: "Miner One",
              available: true,
              ownerAccountId,
            }),
          ],
          canManageAssignments: true,
          canViewAssignments: true,
          children: [{ accountId: childId, name: "Child" }],
        }),
      ),
    );
    render(
      <MemoryRouter
        initialEntries={[
          {
            pathname: `/accounts/${accountId}/children`,
            search: `?assets=${childId}&action=assign`,
          },
        ]}
      >
        <AccountScopeContext.Provider
          value={{
            ...value,
            client: {
              ...client,
              listAccountAssets,
              assignAccountAssets: vi.fn().mockResolvedValue({}),
            } as unknown as AccountScope["client"],
          }}
        >
          <AccountChildren />
        </AccountScopeContext.Provider>
      </MemoryRouter>,
    );
    await user.click(await screen.findByRole("checkbox", { name: "Select Miner One" }));
    await user.click(screen.getByRole("button", { name: "Allocate selected" }));
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Allocate 1 asset" }));
    expect(await screen.findByRole("button", { name: "Allocate assets" })).toBeInTheDocument();
    await waitFor(() =>
      expect(listAccountAssets).toHaveBeenLastCalledWith(
        expect.objectContaining({ ownerAccountId: childId }),
        expect.anything(),
      ),
    );
    expect(pushToast).toHaveBeenCalledExactlyOnceWith({
      message: "Allocated 1 asset to Child.",
      status: STATUSES.success,
    });
    expect(screen.queryByText("Allocated 1 asset to Child.")).not.toBeInTheDocument();
  });
  it("loads the created subclient's mining assets without showing it as unavailable", async () => {
    const user = userEvent.setup(),
      { value, client } = scope();
    const child = create(AccountSchema, {
      ticketingEnabled: true,
      accountId: "88888888-8888-4888-8888-888888888888",
      name: "New subclient",
      depth: 2,
      effectiveCapabilities: 31n,
      capabilityCeiling: 31n,
    });
    client.createAccount.mockResolvedValue({ account: child });
    let finish!: () => void;
    client.listChildren.mockResolvedValueOnce({ accounts: [] }).mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = () => resolve({ accounts: [child] });
        }),
    );
    const listAccountAssets = vi.fn().mockResolvedValue({ assets: [], nextPageToken: "", children: [] });
    mount(<AccountChildren />, {
      ...value,
      client: { ...client, listAccountAssets } as unknown as AccountScope["client"],
    });
    await screen.findByText("No subclients yet");
    await user.click(screen.getByRole("button", { name: "Create subclient" }));
    const dialog = screen.getByRole("dialog", { name: "Create subclient" });
    await user.type(within(dialog).getByLabelText("Subclient name"), child.name);
    await user.click(within(dialog).getByRole("button", { name: "Create subclient" }));
    await waitFor(() => expect(client.listChildren).toHaveBeenCalledTimes(2));
    expect(client.createAccount).toHaveBeenCalledWith({
      accountId,
      name: child.name,
      capabilities: 31n,
      ticketingEnabled: true,
    });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.queryByText("This account is unavailable.")).not.toBeInTheDocument();
    expect(listAccountAssets).not.toHaveBeenCalled();
    await act(async () => finish());
    expect(await screen.findByRole("link", { name: "Invite team member" })).toHaveAttribute(
      "href",
      `/accounts/${child.accountId}/team?invite=1`,
    );
    await waitFor(() =>
      expect(listAccountAssets).toHaveBeenCalledWith(
        expect.objectContaining({ accountId, ownerAccountId: child.accountId }),
        expect.objectContaining({ signal: expect.any(AbortSignal) }),
      ),
    );
    expect(screen.queryByText("This account is unavailable.")).not.toBeInTheDocument();
  });
  it("saves permission changes immediately and rolls a failed update back", async () => {
    const user = userEvent.setup();
    const { value, client } = scope();
    const onSaved = vi.fn();
    let reject!: (error: Error) => void;
    client.configureAccount.mockImplementationOnce(
      () =>
        new Promise((_resolve, rejectRequest) => {
          reject = rejectRequest;
        }),
    );
    mount(
      <ClientPermissions
        child={create(AccountSchema, {
          ticketingEnabled: true,
          accountId: "child",
          name: "Child",
          capabilityCeiling: 31n,
        })}
        onSaved={onSaved}
      />,
      value,
    );
    const permissions = within(screen.getByRole("region", { name: "Client permissions" }));
    expect(permissions.queryByRole("button", { name: "Save permissions" })).not.toBeInTheDocument();
    const changePools = permissions.getByRole("checkbox", { name: "Change pools" });
    await user.click(changePools);
    expect(changePools).not.toBeChecked();
    expect(changePools).toBeDisabled();
    expect(permissions.getByRole("button", { name: "Suspend account" })).toBeDisabled();
    await act(async () => reject(new Error("Could not save permissions")));
    await screen.findByText(/Could not save permissions/);
    expect(changePools).toBeChecked();
    expect(changePools).toBeEnabled();
    expect(onSaved).not.toHaveBeenCalled();
    await user.click(changePools);
    await waitFor(() => expect(onSaved).toHaveBeenCalledOnce());
    expect(changePools).not.toBeChecked();
    expect(client.configureAccount).toHaveBeenLastCalledWith({
      accountId,
      childAccountId: "child",
      capabilities: 27n,
      suspended: false,
      ticketingEnabled: true,
    });
    expect(screen.queryByText(/Could not save permissions/)).not.toBeInTheDocument();
  });

  it("does not refresh another view after a pending permission update finishes", async () => {
    const user = userEvent.setup();
    const { value, client } = scope();
    const onSaved = vi.fn();
    let finish!: () => void;
    client.configureAccount.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = () => resolve({});
        }),
    );
    const view = mount(
      <ClientPermissions
        child={create(AccountSchema, {
          ticketingEnabled: true,
          accountId: "child",
          name: "Child",
          capabilityCeiling: 31n,
        })}
        onSaved={onSaved}
      />,
      value,
    );
    await user.click(screen.getByRole("checkbox", { name: "Change pools" }));
    view.unmount();
    await act(async () => finish());
    expect(onSaved).not.toHaveBeenCalled();
  });

  it.each([false, true])("confirms account status changes separately (suspended=%s)", async (suspended) => {
    const user = userEvent.setup();
    const { value, client } = scope();
    const onSaved = vi.fn();
    const action = suspended ? "Resume account" : "Suspend account";
    mount(
      <ClientPermissions
        child={create(AccountSchema, {
          ticketingEnabled: true,
          accountId: "child",
          name: "Child",
          capabilityCeiling: 31n,
          suspended,
        })}
        onSaved={onSaved}
      />,
      value,
    );
    expect(screen.queryByRole("checkbox", { name: /Suspend account/ })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: action }));
    expect(client.configureAccount).not.toHaveBeenCalled();
    let dialog = screen.getByRole("dialog", { name: suspended ? "Resume Child?" : "Suspend Child?" });
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(client.configureAccount).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: action }));
    dialog = screen.getByRole("dialog", { name: suspended ? "Resume Child?" : "Suspend Child?" });
    await user.click(within(dialog).getByRole("button", { name: action }));
    await waitFor(() => expect(onSaved).toHaveBeenCalledOnce());
    expect(client.configureAccount).toHaveBeenCalledWith({
      accountId,
      childAccountId: "child",
      capabilities: 31n,
      suspended: !suspended,
      ticketingEnabled: true,
    });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("keeps confirmed suspension when permissions change before the parent refresh finishes", async () => {
    const user = userEvent.setup();
    const { value, client } = scope();
    const onSaved = vi.fn();
    mount(
      <ClientPermissions
        child={create(AccountSchema, {
          ticketingEnabled: true,
          accountId: "child",
          name: "Child",
          capabilityCeiling: 31n,
        })}
        onSaved={onSaved}
      />,
      value,
    );
    await user.click(screen.getByRole("button", { name: "Suspend account" }));
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Suspend account" }));
    await waitFor(() => expect(onSaved).toHaveBeenCalledOnce());
    await user.click(screen.getByRole("checkbox", { name: "Change pools" }));
    await waitFor(() => expect(onSaved).toHaveBeenCalledTimes(2));
    expect(client.configureAccount).toHaveBeenLastCalledWith({
      accountId,
      childAccountId: "child",
      capabilities: 27n,
      suspended: true,
      ticketingEnabled: true,
    });
    expect(screen.getByRole("button", { name: "Resume account" })).toBeInTheDocument();
  });

  it("disables immediate permission and status changes in read-only scope", () => {
    const { value, client } = scope();
    mount(
      <ClientPermissions
        child={create(AccountSchema, {
          ticketingEnabled: true,
          accountId: "child",
          name: "Child",
          capabilityCeiling: 31n,
        })}
        onSaved={vi.fn()}
      />,
      { ...value, readOnly: true },
    );
    expect(screen.getByRole("checkbox", { name: "Change pools" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Suspend account" })).toBeDisabled();
    expect(client.configureAccount).not.toHaveBeenCalled();
  });

  it("manages teammate roles without a separate ticket-access control", async () => {
    const user = userEvent.setup();
    const { value } = scope();
    mount(<AccountTeam />, value);
    const row = (await screen.findByText("payout_ops")).closest("tr")!;
    expect(screen.queryByRole("columnheader", { name: "Ticket access" })).not.toBeInTheDocument();
    await user.click(within(row).getByRole("button", { name: "Row actions" }));
    expect(await screen.findByRole("button", { name: "Change role" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /ticket access/i })).not.toBeInTheDocument();
  });

  it("toggles the account ticket feature without changing its capability roles", async () => {
    const user = userEvent.setup();
    const { value, client } = scope();
    mount(
      <ClientPermissions
        child={create(AccountSchema, {
          accountId: "child",
          name: "Child",
          capabilityCeiling: 31n,
          ticketingEnabled: true,
        })}
        onSaved={vi.fn()}
      />,
      value,
    );
    await user.click(screen.getByRole("checkbox", { name: "Ticketing" }));
    await waitFor(() =>
      expect(client.configureAccount).toHaveBeenCalledWith({
        accountId,
        childAccountId: "child",
        capabilities: 31n,
        suspended: false,
        ticketingEnabled: false,
      }),
    );
  });

  it("keeps required access bits while configuring a child", async () => {
    const user = userEvent.setup(),
      { value, client } = scope();
    mount(<AccountChildren />, value);
    const row = (await screen.findByText("Child", { exact: true })).closest("tr")!;
    await user.click(within(row).getByRole("button", { name: "Row actions" }));
    await user.click(await screen.findByRole("button", { name: "Manage permissions" }));
    expect(screen.getByRole("checkbox", { name: "Manage memberships" })).toBeDisabled();
    expect(screen.getByRole("checkbox", { name: "View account data" })).toBeDisabled();
    await user.click(screen.getByRole("checkbox", { name: "Change pools" }));
    await user.click(screen.getByRole("button", { name: "Save permissions" }));
    await waitFor(() =>
      expect(client.configureAccount).toHaveBeenCalledWith({
        accountId,
        childAccountId: "77777777-7777-4777-8777-777777777777",
        capabilities: 27n,
        suspended: false,
        ticketingEnabled: true,
      }),
    );
  });

  it.each([false, true])(
    "expires only the issuing login when a mutation returns Unauthenticated (new login: %s)",
    async (newLogin) => {
      const { Code, ConnectError } = await import("@connectrpc/connect");
      const user = userEvent.setup(),
        { value, client } = scope();
      let reject!: (error: Error) => void;
      client.createInvitation.mockImplementationOnce(
        () =>
          new Promise((_resolve, j) => {
            reject = j;
          }),
      );
      useFleetStore.setState({ auth: { ...initialAuth, isAuthenticated: true, username: "current_user" } });
      mount(<AccountTeam />, value);
      await user.click(await screen.findByRole("button", { name: "Invite team member" }));
      await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Create invitation" }));
      if (newLogin)
        act(() =>
          useFleetStore.setState({
            auth: {
              ...initialAuth,
              isAuthenticated: true,
              username: "new_user",
              sessionGeneration: initialAuth.sessionGeneration + 1,
            },
          }),
        );
      await act(async () => reject(new ConnectError("Session expired", Code.Unauthenticated)));
      expect(useFleetStore.getState().auth.isAuthenticated).toBe(newLogin);
      expect(useFleetStore.getState().auth.username).toBe(newLogin ? "new_user" : "");
    },
  );
  it("invalidates the workspace when a private read is denied", async () => {
    const { Code, ConnectError } = await import("@connectrpc/connect");
    const { value, client } = scope();
    client.listMembers.mockRejectedValueOnce(new ConnectError("membership revoked", Code.PermissionDenied));
    mount(<AccountTeam />, value);
    await waitFor(() => expect(value.invalidateAccount).toHaveBeenCalled());
    expect(screen.queryByText("payout_ops")).not.toBeInTheDocument();
  });
});
