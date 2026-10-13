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
import {
  AccountRole,
  AccountSchema,
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
    createInvitation: vi.fn().mockResolvedValue({ token: "x".repeat(43) }),
    revokeMember: vi.fn().mockResolvedValue({}),
    listAccountKeys: vi.fn().mockResolvedValue({ keys: [], nextPageToken: "" }),
    listAncestorGrants: vi.fn().mockResolvedValue({ grants: [] }),
    createAccountKey: vi.fn().mockResolvedValue({ secret: "one-time-test-secret" }),
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
  it("renders the exact pinned revision and gives a Viewer no pool mutations", async () => {
    const { value, client } = scope(1n);
    mount(<AccountPools />, value);
    expect(await screen.findByRole("button", { name: "Original destination · revision 3" })).toBeInTheDocument();
    expect(client.getPoolRevision).toHaveBeenCalledWith(
      { accountId, poolId, revisionId: oldRevisionId },
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
    expect(screen.queryByRole("button", { name: "Change selection" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Create configuration" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Row actions" })).not.toBeInTheDocument();
  });
  it("submits the selected immutable revision with the current login password", async () => {
    const user = userEvent.setup(),
      { value, client } = scope(7n);
    mount(<AccountPools />, value);
    await user.click(await screen.findByRole("button", { name: "Change selection" }));
    const dialog = screen.getByRole("dialog", { name: "Incoming pool configuration" });
    await user.click(within(dialog).getByRole("button", { name: "Configuration revision" }));
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
  it("shows a portable invitation code once and keeps a fragment link as a convenience", async () => {
    const user = userEvent.setup(),
      { value, client } = scope();
    mount(<AccountTeam />, value);
    await user.click(await screen.findByRole("button", { name: "Create invitation" }));
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Create invitation" }));
    const secret = await screen.findByLabelText("Invitation code");
    expect(secret).toHaveValue("x".repeat(43));
    vi.stubGlobal("isSecureContext", true);
    const copy = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue();
    await user.click(screen.getByRole("button", { name: "Copy invitation code" }));
    expect(copy).toHaveBeenLastCalledWith("x".repeat(43));
    await user.click(screen.getByRole("button", { name: "Copy invitation link (optional)" }));
    expect(copy).toHaveBeenLastCalledWith(`${window.location.origin}/account-invitation#token=${"x".repeat(43)}`);
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
      await user.click(await screen.findByRole("button", { name: "Create invitation" }));
      await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Create invitation" }));
      await screen.findByLabelText("Invitation code");
      await user.click(screen.getByRole("button", { name: "Copy invitation code" }));
      expect(await screen.findByRole("button", { name: "Copied" })).toBeInTheDocument();
      await user.click(screen.getByRole("button", { name: "Copy invitation link (optional)" }));
      await screen.findByRole("button", { name: "Link copied" });
      expect(copied).toEqual(["x".repeat(43), `${window.location.origin}/account-invitation#token=${"x".repeat(43)}`]);
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
      await user.click(await screen.findByRole("button", { name: "Create invitation" }));
      await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Create invitation" }));
      await screen.findByLabelText("Invitation code");
      await user.click(screen.getByRole("button", { name: "Copy invitation code" }));
      await screen.findByText("Copy failed. Select and copy the value above.");
      await user.click(screen.getByRole("button", { name: "Copy invitation link (optional)" }));
      await screen.findByText("Copy failed. Share the invitation code instead.");
      expect(screen.getByLabelText("Invitation code")).toHaveValue("x".repeat(43));
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
  it("creates an account-only key by default and discards its secret on dismiss", async () => {
    const user = userEvent.setup(),
      { value, client } = scope(1n);
    mount(<AccountAccess />, value);
    await user.click(await screen.findByRole("button", { name: "Create account key" }));
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
      await user.click(within(dialog).getByRole("checkbox", { name: "Keep the saved password for this priority" }));
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
    await user.click(within(backup).getByRole("checkbox", { name: "Keep the saved password for this priority" }));
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
  it("keeps required access bits while configuring a child", async () => {
    const user = userEvent.setup(),
      { value, client } = scope();
    mount(<AccountChildren />, value);
    const row = (await screen.findByText("Child", { exact: true })).closest("tr")!;
    await user.click(within(row).getByRole("button", { name: "Manage permissions" }));
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
      await user.click(await screen.findByRole("button", { name: "Create invitation" }));
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
