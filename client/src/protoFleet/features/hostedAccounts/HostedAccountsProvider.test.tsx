import { MemoryRouter, Route, Routes } from "react-router-dom";
import { act, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { hostedAccountsClient } from "./api";
import { useHostedAccounts } from "./HostedAccountsContext";
import { HostedAccountsProvider } from "./HostedAccountsProvider";
import { HostWorkspaceGate } from "./HostWorkspaceGate";
import { AccountSchema, ListAccountsResponseSchema } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import { useFleetStore } from "@/protoFleet/store";

const accountId = "11111111-1111-4111-8111-111111111111";
const initialAuth = useFleetStore.getState().auth;
afterEach(() => {
  useFleetStore.setState({ auth: initialAuth });
  vi.restoreAllMocks();
});
const login = (generation: number, permissions: string[] = []) =>
  act(() => {
    useFleetStore.setState({
      auth: { ...initialAuth, isAuthenticated: true, sessionGeneration: generation, permissions },
    });
  });
const Reader = () => {
  const directory = useHostedAccounts()!;
  return <div>{directory.loading ? "Loading" : directory.accounts.map((a) => a.name).join(",")}</div>;
};
describe("account directory and host workspace isolation", () => {
  it("hides the old login's directory and ignores its delayed response", async () => {
    let resolve!: (value: Awaited<ReturnType<typeof hostedAccountsClient.listAccounts>>) => void;
    let oldSignal!: AbortSignal;
    vi.spyOn(hostedAccountsClient, "listAccounts")
      .mockImplementationOnce((_request, options) => {
        oldSignal = options!.signal!;
        return new Promise((r) => {
          resolve = r;
        }) as ReturnType<typeof hostedAccountsClient.listAccounts>;
      })
      .mockResolvedValueOnce({
        accounts: [create(AccountSchema, { accountId, name: "New login" })],
        hostAccess: false,
      } as Awaited<ReturnType<typeof hostedAccountsClient.listAccounts>>);
    login(10);
    render(
      <HostedAccountsProvider>
        <Reader />
      </HostedAccountsProvider>,
    );
    login(11);
    expect(oldSignal.aborted).toBe(true);
    expect(screen.getByText("Loading")).toBeInTheDocument();
    await screen.findByText("New login");
    await act(async () =>
      resolve(
        create(ListAccountsResponseSchema, {
          accounts: [create(AccountSchema, { name: "Old login" })],
          hostAccess: true,
        }),
      ),
    );
    expect(screen.queryByText("Old login")).not.toBeInTheDocument();
  });
  it("does not mount host providers for a membership-only login", async () => {
    vi.spyOn(hostedAccountsClient, "listAccounts").mockResolvedValue({
      accounts: [create(AccountSchema, { accountId })],
      hostAccess: false,
    } as Awaited<ReturnType<typeof hostedAccountsClient.listAccounts>>);
    const mountHost = vi.fn();
    const HostPage = () => {
      mountHost();
      return <div>Host data</div>;
    };
    login(20);
    render(
      <MemoryRouter initialEntries={["/dashboard"]}>
        <HostedAccountsProvider>
          <Routes>
            <Route
              path="/dashboard"
              element={
                <HostWorkspaceGate>
                  <HostPage />
                </HostWorkspaceGate>
              }
            />
            <Route path={`/accounts/${accountId}/settings`} element={<div>Customer workspace</div>} />
          </Routes>
        </HostedAccountsProvider>
      </MemoryRouter>,
    );
    await screen.findByText("Customer workspace");
    expect(mountHost).not.toHaveBeenCalled();
  });
  it("preserves existing host access while the account directory is unavailable", async () => {
    vi.spyOn(hostedAccountsClient, "listAccounts").mockRejectedValue(new ConnectError("Unavailable", Code.Unavailable));
    login(30, ["miner:read"]);
    render(
      <MemoryRouter>
        <HostedAccountsProvider>
          <HostWorkspaceGate>
            <div>Host workspace</div>
          </HostWorkspaceGate>
        </HostedAccountsProvider>
      </MemoryRouter>,
    );
    expect(screen.getByText("Host workspace")).toBeInTheDocument();
    await waitFor(() => expect(hostedAccountsClient.listAccounts).toHaveBeenCalled());
  });
  it("retains this login's directory on a transient focus failure", async () => {
    vi.spyOn(hostedAccountsClient, "listAccounts")
      .mockResolvedValueOnce(
        create(ListAccountsResponseSchema, {
          accounts: [create(AccountSchema, { accountId, name: "Customer" })],
          hostAccess: false,
        }),
      )
      .mockRejectedValueOnce(new ConnectError("Temporarily unavailable", Code.Unavailable));
    login(35);
    render(
      <HostedAccountsProvider>
        <Reader />
      </HostedAccountsProvider>,
    );
    await screen.findByText("Customer");
    act(() => window.dispatchEvent(new Event("focus")));
    await waitFor(() => expect(hostedAccountsClient.listAccounts).toHaveBeenCalledTimes(2));
    expect(screen.getByText("Customer")).toBeInTheDocument();
  });
  it("ends an expired login when the directory rejects its session", async () => {
    vi.spyOn(hostedAccountsClient, "listAccounts").mockRejectedValue(
      new ConnectError("Session expired", Code.Unauthenticated),
    );
    login(40);
    render(
      <HostedAccountsProvider>
        <Reader />
      </HostedAccountsProvider>,
    );
    await waitFor(() => expect(useFleetStore.getState().auth.isAuthenticated).toBe(false));
  });
});
