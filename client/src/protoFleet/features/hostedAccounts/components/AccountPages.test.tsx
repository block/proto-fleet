import type { ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";

import { type AccountScope, AccountScopeContext } from "../AccountScope";
import AccountAccess from "./AccountAccess";
import AccountChildren from "./AccountChildren";
import AccountPools from "./AccountPools";
import AccountTeam from "./AccountTeam";
import { ClientPermissions } from "./ClientPermissions";
import {
  AccountRole,
  AccountSchema,
  InvitationSchema,
  MemberSchema,
  PoolRevisionSchema,
} from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import { useFleetStore } from "@/protoFleet/store";

const initialAuth = useFleetStore.getState().auth;
const accountId = "11111111-1111-4111-8111-111111111111";
const poolId = "22222222-2222-4222-8222-222222222222";
const oldRevisionId = "33333333-3333-4333-8333-333333333333";
const newRevisionId = "44444444-4444-4444-8444-444444444444";
const getPools = () => {
  const latest = create(PoolRevisionSchema, {
    poolId,
    revisionId: newRevisionId,
    number: 4n,
    name: "New destination",
    slots: [
      { url: "stratum+tcp://new.example:3333", username: "payout", workerTemplate: "{miner_name}", hasPassword: true },
    ],
  });
  const pinned = create(PoolRevisionSchema, {
    poolId,
    revisionId: oldRevisionId,
    number: 3n,
    name: "Original destination",
    slots: [{ url: "stratum+tcp://old.example:3333", username: "payout", hasPassword: true }],
  });
  return { latest, pinned };
};
function scope(mask = 31n) {
  const { latest, pinned } = getPools();
  const client = {
    listPoolConfigurations: vi.fn().mockResolvedValue({ configurations: [latest], nextPageToken: "" }),
    getIncomingPoolProfile: vi.fn().mockResolvedValue({ profile: { poolId, revisionId: oldRevisionId, revision: 1n } }),
    getPoolRevision: vi.fn().mockResolvedValue({ revision: pinned }),
    revisePoolConfiguration: vi.fn().mockResolvedValue({}),
    createAccount: vi.fn().mockResolvedValue({}),
    configureAccount: vi.fn().mockResolvedValue({}),
    listChildren: vi.fn().mockResolvedValue({
      accounts: [
        create(AccountSchema, {
          accountId: "77777777-7777-4777-8777-777777777777",
          name: "Child",
          depth: 2,
          capabilityCeiling: 31n,
        }),
      ],
    }),
    setIncomingPoolProfile: vi.fn().mockResolvedValue({}),
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
    createInvitation: vi.fn().mockResolvedValue({ token: "K7MP-4X9R-6TWC-8NQF" }),
    revokeMember: vi.fn().mockResolvedValue({}),
    revokeInvitation: vi.fn().mockResolvedValue({}),
    listAccountKeys: vi.fn().mockResolvedValue({ keys: [], nextPageToken: "" }),
    listAncestorGrants: vi.fn().mockResolvedValue({ grants: [] }),
    createAccountKey: vi.fn().mockResolvedValue({ secret: "one-time-test-secret" }),
    renameAccount: vi.fn().mockResolvedValue({}),
  };
  const value: AccountScope = {
    account: create(AccountSchema, {
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
    { page: <AccountPools />, controls: ["Add pool", "Change selection"] },
    { page: <AccountAccess />, controls: ["Create API key", "Grant ancestor access"] },
    { page: <AccountTeam />, controls: ["Invite team member"] },
    { page: <AccountChildren />, controls: ["Create subclient"] },
  ])("keeps administrator mutation controls visible and disabled in preview: $controls", async ({ page, controls }) => {
    const { value } = scope();
    mount(page, { ...value, readOnly: true });
    for (const name of controls) expect(await screen.findByRole("button", { name })).toBeDisabled();
  });
  it("renders the exact pinned revision and gives a Viewer no pool mutations", async () => {
    const { value, client } = scope(1n);
    mount(<AccountPools />, value);
    expect(await screen.findByRole("button", { name: "Original destination · revision 3" })).toBeInTheDocument();
    expect(screen.getByText("Stored pool profile")).toBeInTheDocument();
    expect(screen.getByText("Pools", { exact: true })).toBeInTheDocument();
    for (const title of ["Name", "URL", "Username"])
      expect(screen.getByRole("columnheader", { name: title })).toBeInTheDocument();
    expect(screen.getByText("stratum+tcp://new.example:3333")).toBeInTheDocument();
    expect(screen.getByText("payout", { exact: true })).toBeInTheDocument();
    expect(screen.queryByText("Pools for newly assigned miners.")).not.toBeInTheDocument();
    expect(client.getPoolRevision).toHaveBeenCalledWith(
      { accountId, poolId, revisionId: oldRevisionId },
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
    expect(screen.queryByRole("button", { name: "Change selection" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Add pool" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Row actions" })).not.toBeInTheDocument();
  });
  it("submits the selected immutable revision with the current login password", async () => {
    const user = userEvent.setup(),
      { value, client } = scope(7n);
    mount(<AccountPools />, value);
    await user.click(await screen.findByRole("button", { name: "Change selection" }));
    const dialog = screen.getByRole("dialog", { name: "Stored pool profile" });
    expect(
      within(dialog).getByText(
        "This changes the stored profile. It does not apply pool settings to existing or newly assigned miners or modules.",
      ),
    ).toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: "Revision" }));
    await user.click(await screen.findByRole("option", { name: "New destination · revision 4" }));
    await user.type(within(dialog).getByLabelText("Your Fleet password"), "test-current-password");
    await user.click(within(dialog).getByRole("button", { name: "Confirm selection" }));
    await waitFor(() =>
      expect(client.setIncomingPoolProfile).toHaveBeenCalledWith({
        accountId,
        revisionId: newRevisionId,
        password: "test-current-password",
      }),
    );
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
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
    expect(secret).toHaveValue("K7MP-4X9R-6TWC-8NQF");
    vi.stubGlobal("isSecureContext", true);
    const copy = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue();
    await user.click(screen.getByRole("button", { name: "Copy invitation code" }));
    expect(copy).toHaveBeenLastCalledWith("K7MP-4X9R-6TWC-8NQF");
    await user.click(screen.getByRole("button", { name: "Copy link" }));
    expect(copy).toHaveBeenLastCalledWith(`${window.location.origin}/account-invitation#token=K7MP-4X9R-6TWC-8NQF`);
    expect(client.createInvitation).toHaveBeenCalledWith({
      accountId,
      role: AccountRole.VIEWER,
      expiresInSeconds: 86400,
    });
    await user.click(screen.getByRole("button", { name: "Done" }));
    expect(screen.queryByLabelText("Invitation code")).not.toBeInTheDocument();
    expect(screen.queryByLabelText(/email/i)).not.toBeInTheDocument();
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
      expect(await screen.findByRole("button", { name: "Copied" })).toBeInTheDocument();
      await user.click(screen.getByRole("button", { name: "Copy link" }));
      await screen.findByRole("button", { name: "Link copied" });
      expect(copied).toEqual([
        "K7MP-4X9R-6TWC-8NQF",
        `${window.location.origin}/account-invitation#token=K7MP-4X9R-6TWC-8NQF`,
      ]);
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
      await user.click(screen.getByRole("button", { name: "Copy link" }));
      await screen.findByText("Couldn’t copy the link. Share the code instead.");
      expect(screen.getByLabelText("Invitation code")).toHaveValue("K7MP-4X9R-6TWC-8NQF");
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
    expect(within(details).queryByText("Active")).not.toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Your permissions" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Ancestor access" })).toBeInTheDocument();
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
    await user.type(screen.getByLabelText("Key name"), "Reporting");
    await user.click(screen.getByRole("button", { name: "Create key" }));
    expect(await screen.findByLabelText("Secret")).toHaveValue("one-time-test-secret");
    expect(client.createAccountKey).toHaveBeenCalledWith(
      expect.objectContaining({
        accountId,
        name: "Reporting",
        capabilities: 1n,
        descendantAccountIds: [],
        includeFutureDescendants: false,
      }),
    );
    await user.click(screen.getByRole("button", { name: "Done" }));
    expect(screen.queryByLabelText("Secret")).not.toBeInTheDocument();
  });

  it("saves an edited revision with an unchanged password omitted", async () => {
    const user = userEvent.setup(),
      { value, client } = scope();
    mount(<AccountPools />, value);
    const row = (await screen.findByText("New destination", { exact: true })).closest("tr")!;
    await user.click(within(row).getByRole("button", { name: "Row actions" }));
    await user.click(await screen.findByRole("button", { name: "Edit configuration" }));
    const dialog = screen.getByRole("dialog", { name: "Edit pool configuration" });
    expect(within(dialog).queryByLabelText("Pool password (optional)")).not.toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: "Save new revision" }));
    await waitFor(() =>
      expect(client.revisePoolConfiguration).toHaveBeenCalledWith({
        accountId,
        poolId,
        expectedRevisionId: newRevisionId,
        configuration: {
          name: "New destination",
          slots: [{ url: "stratum+tcp://new.example:3333", username: "payout", workerTemplate: "{miner_name}" }],
        },
      }),
    );
  });
  it.each(["", "replacement-test-password"])(
    "requires explicit password replacement/clearing after changing destination (%s)",
    async (replacement) => {
      const user = userEvent.setup(),
        { value, client } = scope();
      mount(<AccountPools />, value);
      const row = (await screen.findByText("New destination", { exact: true })).closest("tr")!;
      await user.click(within(row).getByRole("button", { name: "Row actions" }));
      await user.click(await screen.findByRole("button", { name: "Edit configuration" }));
      const dialog = screen.getByRole("dialog", { name: "Edit pool configuration" });
      await user.clear(within(dialog).getByLabelText("Pool URL"));
      await user.type(within(dialog).getByLabelText("Pool URL"), "stratum+tcp://changed.example:3333");
      expect(within(dialog).getByRole("button", { name: "Save new revision" })).toBeDisabled();
      await user.click(within(dialog).getByRole("checkbox", { name: "Keep saved password" }));
      if (replacement) await user.type(within(dialog).getByLabelText("Pool password (optional)"), replacement);
      await user.click(within(dialog).getByRole("button", { name: "Save new revision" }));
      await waitFor(() =>
        expect(client.revisePoolConfiguration).toHaveBeenCalledWith(
          expect.objectContaining({
            expectedRevisionId: newRevisionId,
            configuration: expect.objectContaining({
              slots: [expect.objectContaining({ url: "stratum+tcp://changed.example:3333", password: replacement })],
            }),
          }),
        ),
      );
    },
  );
  it("does not retain a shifted backup's password from another priority", async () => {
    const user = userEvent.setup(),
      { value, client } = scope();
    const { latest } = getPools();
    latest.slots.push(
      ...["backup-one", "backup-two"].map((name) => ({
        ...latest.slots[0],
        url: `stratum+tcp://${name}.example:3333`,
      })),
    );
    client.listPoolConfigurations.mockResolvedValue({ configurations: [latest], nextPageToken: "" });
    mount(<AccountPools />, value);
    const row = (await screen.findByText("New destination", { exact: true })).closest("tr")!;
    await user.click(within(row).getByRole("button", { name: "Row actions" }));
    await user.click(await screen.findByRole("button", { name: "Edit configuration" }));
    await user.click(
      within(screen.getByRole("group", { name: "Backup 1" })).getByRole("button", { name: "Remove backup" }),
    );
    expect(screen.getByRole("button", { name: "Save new revision" })).toBeDisabled();
    const backup = screen.getByRole("group", { name: "Backup 1" });
    expect(within(backup).getByLabelText("Pool URL")).toHaveValue("stratum+tcp://backup-two.example:3333");
    await user.click(within(backup).getByRole("checkbox", { name: "Keep saved password" }));
    await user.type(within(backup).getByLabelText("Pool password (optional)"), "backup-test-secret");
    await user.click(screen.getByRole("button", { name: "Save new revision" }));
    await waitFor(() =>
      expect(client.revisePoolConfiguration).toHaveBeenCalledWith(
        expect.objectContaining({
          configuration: expect.objectContaining({
            slots: [
              expect.not.objectContaining({ password: expect.anything() }),
              expect.objectContaining({ url: "stratum+tcp://backup-two.example:3333", password: "backup-test-secret" }),
            ],
          }),
        }),
      ),
    );
  });
  it("shows the assignment confirmation in the child inventory it returns to", async () => {
    const { value, client } = scope();
    const childId = "77777777-7777-4777-8777-777777777777";
    render(
      <MemoryRouter
        initialEntries={[
          {
            pathname: `/accounts/${accountId}/children`,
            search: `?assets=${childId}`,
            state: { assetNotice: "Assigned 1 asset to Child." },
          },
        ]}
      >
        <AccountScopeContext.Provider
          value={{
            ...value,
            client: {
              ...client,
              listAccountAssets: vi.fn().mockResolvedValue({ assets: [], nextPageToken: "", children: [] }),
            } as unknown as AccountScope["client"],
          }}
        >
          <AccountChildren />
        </AccountScopeContext.Provider>
      </MemoryRouter>,
    );
    expect(await screen.findByRole("status")).toHaveTextContent("Assigned 1 asset to Child.");
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
        child={create(AccountSchema, { accountId: "child", name: "Child", capabilityCeiling: 31n })}
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
        child={create(AccountSchema, { accountId: "child", name: "Child", capabilityCeiling: 31n })}
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
        child={create(AccountSchema, { accountId: "child", name: "Child", capabilityCeiling: 31n, suspended })}
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
    });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("keeps confirmed suspension when permissions change before the parent refresh finishes", async () => {
    const user = userEvent.setup();
    const { value, client } = scope();
    const onSaved = vi.fn();
    mount(
      <ClientPermissions
        child={create(AccountSchema, { accountId: "child", name: "Child", capabilityCeiling: 31n })}
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
    });
    expect(screen.getByRole("button", { name: "Resume account" })).toBeInTheDocument();
  });

  it("disables immediate permission and status changes in read-only scope", () => {
    const { value, client } = scope();
    mount(
      <ClientPermissions
        child={create(AccountSchema, { accountId: "child", name: "Child", capabilityCeiling: 31n })}
        onSaved={vi.fn()}
      />,
      { ...value, readOnly: true },
    );
    expect(screen.getByRole("checkbox", { name: "Change pools" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Suspend account" })).toBeDisabled();
    expect(client.configureAccount).not.toHaveBeenCalled();
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
