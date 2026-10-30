import { MemoryRouter } from "react-router-dom";
import { act, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import userEvent from "@testing-library/user-event";

import { type AccountScope, AccountScopeContext } from "../AccountScope";
import { ReplaceHashboardDialog } from "./ReplaceHashboardDialog";
import {
  AccountAssetSchema,
  AccountSchema,
  ListAccountAssetsResponseSchema,
  MiningAssetKind,
} from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import { pushToast, STATUSES } from "@/shared/features/toaster";

vi.mock("@/shared/features/toaster", () => ({
  pushToast: vi.fn(),
  STATUSES: { success: "success" },
}));

let reachEnd: () => void;
beforeEach(() => {
  vi.mocked(pushToast).mockClear();
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(20, 20, 240, 40));
  vi.spyOn(HTMLElement.prototype, "getClientRects").mockReturnValue(
    Object.assign([new DOMRect(20, 20, 240, 40)], { item: () => null }),
  );
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      constructor(callback: ConstructorParameters<typeof IntersectionObserver>[0]) {
        reachEnd = () =>
          callback([{ isIntersecting: true } as IntersectionObserverEntry], this as unknown as IntersectionObserver);
      }
      observe = vi.fn();
      disconnect = vi.fn();
    },
  );
});
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

const root = "11111111-1111-4111-8111-111111111111";
const oldBoard = create(AccountAssetSchema, {
  kind: MiningAssetKind.HASHBOARD,
  id: 11n,
  revision: 4n,
  name: "Old board",
});
const replacement = create(AccountAssetSchema, {
  kind: MiningAssetKind.HASHBOARD,
  id: 22n,
  revision: 7n,
  name: "Spare board",
  available: true,
  ownerAccountId: root,
});

function mount(
  error?: ConnectError,
  readOnly = false,
  listAccountAssets = vi.fn().mockResolvedValue(create(ListAccountAssetsResponseSchema, { assets: [replacement] })),
) {
  const client = {
    listAccountAssets,
    replaceAccountHashboard: error ? vi.fn().mockRejectedValue(error) : vi.fn().mockResolvedValue({}),
  };
  const callbacks = { onDismiss: vi.fn(), onReplaced: vi.fn(), onConflict: vi.fn() };
  const scope: AccountScope = {
    account: create(AccountSchema, { accountId: root, depth: 0, effectiveCapabilities: 31n }),
    client: client as unknown as AccountScope["client"],
    signal: new AbortController().signal,
    readOnly,
    refreshAccount: vi.fn(),
    invalidateAccount: vi.fn(),
  };
  render(
    <MemoryRouter>
      <AccountScopeContext.Provider value={scope}>
        <ReplaceHashboardDialog oldBoard={oldBoard} {...callbacks} />
      </AccountScopeContext.Provider>
    </MemoryRouter>,
  );
  return { client, ...callbacks };
}

async function chooseReplacement() {
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "Replacement hashboard" }));
  await user.click(await screen.findByRole("option", { name: "Spare board" }));
  return user;
}

describe("manual hashboard replacement", () => {
  it("sends the exact reviewed board identities and revisions", async () => {
    const { client, onReplaced } = mount();
    expect(screen.getByRole("button", { name: "Transfer allocation" })).toBeDisabled();
    const user = await chooseReplacement();
    expect(client.listAccountAssets).toHaveBeenCalledWith(
      expect.objectContaining({
        accountId: root,
        ownerAccountId: root,
        hashboardsOnly: true,
        replacementFor: { id: oldBoard.id, expectedRevision: oldBoard.revision },
      }),
      expect.anything(),
    );
    await user.click(screen.getByRole("button", { name: "Transfer allocation" }));
    await waitFor(() => expect(onReplaced).toHaveBeenCalledOnce());
    expect(pushToast).toHaveBeenCalledExactlyOnceWith({
      message: "Hashboard allocation transferred.",
      status: STATUSES.success,
    });
    expect(screen.queryByText("Hashboard allocation transferred.")).not.toBeInTheDocument();
    expect(client.replaceAccountHashboard).toHaveBeenCalledWith({
      accountId: root,
      oldBoard: { id: 11n, expectedRevision: 4n },
      replacementBoard: { id: 22n, expectedRevision: 7n },
    });
  });

  it("requires a fresh review after an allocation conflict", async () => {
    const { onConflict, onReplaced } = mount(new ConnectError("Allocation changed", Code.FailedPrecondition));
    const user = await chooseReplacement();
    await user.click(screen.getByRole("button", { name: "Transfer allocation" }));
    await waitFor(() => expect(onConflict).toHaveBeenCalledOnce());
    expect(onReplaced).not.toHaveBeenCalled();
    expect(pushToast).not.toHaveBeenCalled();
  });

  it("keeps a policy failure visible without reporting success", async () => {
    const { onConflict, onReplaced } = mount(
      new ConnectError("Replacement is outside the site policy", Code.InvalidArgument),
    );
    const user = await chooseReplacement();
    await user.click(screen.getByRole("button", { name: "Transfer allocation" }));
    expect(await screen.findByText("Replacement is outside the site policy")).toBeInTheDocument();
    expect(onConflict).not.toHaveBeenCalled();
    expect(onReplaced).not.toHaveBeenCalled();
    expect(pushToast).not.toHaveBeenCalled();
  });

  it("prevents changes in a read-only scope", async () => {
    const { client } = mount(undefined, true);
    await chooseReplacement();
    expect(screen.getByRole("button", { name: "Transfer allocation" })).toBeDisabled();
    expect(client.replaceAccountHashboard).not.toHaveBeenCalled();
  });

  it("appends and deduplicates pages, then transfers the selected board's reviewed revision", async () => {
    const nextBoard = create(AccountAssetSchema, { ...replacement, id: 23n, revision: 9n, name: "Next board" });
    const list = vi
      .fn()
      .mockResolvedValueOnce(
        create(ListAccountAssetsResponseSchema, { assets: [replacement], nextPageToken: "page-2" }),
      )
      .mockResolvedValueOnce(create(ListAccountAssetsResponseSchema, { assets: [replacement, nextBoard] }));
    const { client } = mount(undefined, false, list);
    const user = userEvent.setup();
    expect(screen.queryByRole("textbox", { name: "Search hashboards" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Next" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Previous" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Replacement hashboard" }));
    await screen.findByRole("option", { name: "Spare board" });
    act(() => {
      reachEnd();
      reachEnd();
    });
    await screen.findByRole("option", { name: "Next board" });
    expect(screen.getAllByRole("option")).toHaveLength(2);
    expect(list).toHaveBeenCalledTimes(2);
    expect(list).toHaveBeenLastCalledWith(expect.objectContaining({ pageToken: "page-2" }), expect.anything());
    act(() => reachEnd());
    expect(list).toHaveBeenCalledTimes(2);
    await user.click(screen.getByRole("option", { name: "Next board" }));
    await user.click(screen.getByRole("button", { name: "Transfer allocation" }));
    await waitFor(() =>
      expect(client.replaceAccountHashboard).toHaveBeenCalledWith(
        expect.objectContaining({
          replacementBoard: { id: 23n, expectedRevision: 9n },
        }),
      ),
    );
  });

  it("cancels an old page and ignores its late result when the search changes", async () => {
    let resolvePage!: (response: ReturnType<typeof create<typeof ListAccountAssetsResponseSchema>>) => void;
    const searchedBoard = create(AccountAssetSchema, { ...replacement, id: 33n, name: "Search result" });
    const list = vi
      .fn()
      .mockResolvedValueOnce(
        create(ListAccountAssetsResponseSchema, { assets: [replacement], nextPageToken: "page-2" }),
      )
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            resolvePage = resolve;
          }),
      )
      .mockResolvedValueOnce(create(ListAccountAssetsResponseSchema, { assets: [searchedBoard] }));
    mount(undefined, false, list);
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Replacement hashboard" }));
    await screen.findByRole("option", { name: "Spare board" });
    act(() => reachEnd());
    await waitFor(() => expect(list).toHaveBeenCalledTimes(2));
    const oldSignal = list.mock.calls[1][1].signal as AbortSignal;
    expect(screen.getByRole("option", { name: "Spare board" })).toBeInTheDocument();
    await user.type(screen.getByRole("textbox", { name: "Search hashboards" }), "Search");
    expect(oldSignal.aborted).toBe(true);
    expect(screen.queryByRole("option", { name: "Spare board" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Transfer allocation" })).toBeDisabled();
    await screen.findByRole("option", { name: "Search result" });
    expect(list).toHaveBeenLastCalledWith(
      expect.objectContaining({ search: "Search", pageToken: "" }),
      expect.anything(),
    );
    await act(async () => resolvePage(create(ListAccountAssetsResponseSchema, { assets: [replacement] })));
    expect(screen.queryByRole("option", { name: "Spare board" })).not.toBeInTheDocument();
    expect(screen.getByRole("option", { name: "Search result" })).toBeInTheDocument();
  });

  it("retries the failed page without losing previous pages", async () => {
    const nextBoard = create(AccountAssetSchema, { ...replacement, id: 23n, name: "Next board" });
    const list = vi
      .fn()
      .mockResolvedValueOnce(
        create(ListAccountAssetsResponseSchema, { assets: [replacement], nextPageToken: "page-2" }),
      )
      .mockRejectedValueOnce(new ConnectError("Could not load boards", Code.Unavailable))
      .mockResolvedValueOnce(create(ListAccountAssetsResponseSchema, { assets: [nextBoard] }));
    mount(undefined, false, list);
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Replacement hashboard" }));
    await screen.findByRole("option", { name: "Spare board" });
    act(() => reachEnd());
    expect(await screen.findByRole("alert")).toHaveTextContent("Could not load boards");
    act(() => reachEnd());
    expect(list).toHaveBeenCalledTimes(2);
    await user.click(screen.getByRole("button", { name: "Retry" }));
    await screen.findByRole("option", { name: "Next board" });
    expect(screen.getByRole("option", { name: "Spare board" })).toBeInTheDocument();
    expect(list).toHaveBeenLastCalledWith(expect.objectContaining({ pageToken: "page-2" }), expect.anything());
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("reaches retry by keyboard and keeps Tab navigation inside the dialog", async () => {
    const list = vi
      .fn()
      .mockRejectedValueOnce(new ConnectError("Could not load boards", Code.Unavailable))
      .mockResolvedValueOnce(create(ListAccountAssetsResponseSchema, { assets: [replacement] }));
    mount(undefined, false, list);
    const user = userEvent.setup();
    const trigger = screen.getByRole("button", { name: "Replacement hashboard" });
    await user.click(trigger);
    await screen.findByRole("alert");
    const search = screen.getByRole("textbox", { name: "Search hashboards" });
    expect(search).toHaveFocus();
    await user.tab();
    expect(screen.getByRole("button", { name: "Retry" })).toHaveFocus();
    await user.tab({ shift: true });
    expect(search).toHaveFocus();
    await user.tab({ shift: true });
    expect(trigger).toHaveFocus();
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
    await user.keyboard("{Enter}");
    await user.tab();
    await user.tab();
    expect(screen.getByRole("button", { name: "Cancel" })).toHaveFocus();
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
    await user.tab();
    expect(trigger).toHaveFocus();
    await user.keyboard("{Enter}");
    await user.tab();
    await user.keyboard("{Enter}");
    expect(await screen.findByRole("option", { name: "Spare board" })).toBeInTheDocument();
    expect(list).toHaveBeenCalledTimes(2);
    expect(screen.getByRole("textbox", { name: "Search hashboards" })).toHaveFocus();
    await user.keyboard("{ArrowDown}{Enter}");
    expect(screen.getByRole("button", { name: "Transfer allocation" })).toBeEnabled();
  });

  it("finishes a pending search while closed and retains it when reopened", async () => {
    const { client } = mount();
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Replacement hashboard" }));
    await user.type(screen.getByRole("textbox", { name: "Search hashboards" }), "Spare");
    await user.click(screen.getByText("Replace hashboard", { exact: true }));
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
    await waitFor(() =>
      expect(client.listAccountAssets).toHaveBeenLastCalledWith(
        expect.objectContaining({ search: "Spare", pageToken: "" }),
        expect.anything(),
      ),
    );
    await user.click(screen.getByRole("button", { name: "Replacement hashboard" }));
    expect(screen.getByRole("textbox", { name: "Search hashboards" })).toHaveValue("Spare");
    expect(await screen.findByRole("option", { name: "Spare board" })).toBeInTheDocument();
  });
});
