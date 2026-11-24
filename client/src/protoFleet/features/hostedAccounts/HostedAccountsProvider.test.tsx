import { useEffect } from "react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { act, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { hostedAccountsClient } from "./api";
import { useHostedAccounts } from "./HostedAccountsContext";
import { HostedAccountsProvider } from "./HostedAccountsProvider";
import { HostWorkspaceGate } from "./HostWorkspaceGate";
import { AccountSchema, ListAccountsResponseSchema } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import { ListHostReportingAccountsResponseSchema } from "@/protoFleet/api/generated/hosted/v1/reporting_pb";
import { useFleetStore } from "@/protoFleet/store";

const accountId = "11111111-1111-4111-8111-111111111111";
const initialAuth = useFleetStore.getState().auth;
beforeEach(() => {
  vi.spyOn(hostedAccountsClient, "listHostReportingAccounts").mockResolvedValue(
    create(ListHostReportingAccountsResponseSchema),
  );
});
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
  it("clears reporting navigation on a new login and ignores a late authority result", async () => {
    vi.spyOn(hostedAccountsClient, "listAccounts").mockResolvedValue(
      create(ListAccountsResponseSchema, { hostAccess: true }),
    );
    let resolve!: (value: Awaited<ReturnType<typeof hostedAccountsClient.listHostReportingAccounts>>) => void;
    vi.mocked(hostedAccountsClient.listHostReportingAccounts)
      .mockImplementationOnce(
        () =>
          new Promise((r) => {
            resolve = r;
          }),
      )
      .mockResolvedValue(create(ListHostReportingAccountsResponseSchema));
    const ReportingReader = () => (
      <output>{useHostedAccounts()!.reportingAccess ? "Reporting available" : "No reporting access"}</output>
    );
    login(50);
    render(
      <HostedAccountsProvider>
        <ReportingReader />
      </HostedAccountsProvider>,
    );
    await waitFor(() => expect(hostedAccountsClient.listHostReportingAccounts).toHaveBeenCalledTimes(1));
    login(51);
    expect(screen.getByText("No reporting access")).toBeInTheDocument();
    await waitFor(() => expect(hostedAccountsClient.listHostReportingAccounts).toHaveBeenCalledTimes(2));
    await act(async () =>
      resolve(create(ListHostReportingAccountsResponseSchema, { accounts: [{ accountId, canCreate: true }] })),
    );
    expect(screen.getByText("No reporting access")).toBeInTheDocument();
  });

  it("loads reporting authority independently of client management", async () => {
    vi.spyOn(hostedAccountsClient, "listAccounts").mockResolvedValue(
      create(ListAccountsResponseSchema, { hostAccess: true }),
    );
    vi.mocked(hostedAccountsClient.listHostReportingAccounts).mockResolvedValue(
      create(ListHostReportingAccountsResponseSchema, { accounts: [{ accountId, canCreate: true }] }),
    );
    const ReportingReader = () => (
      <output>{useHostedAccounts()!.reportingAccess ? "Reporting available" : "No reporting access"}</output>
    );
    login(52);
    render(
      <HostedAccountsProvider>
        <ReportingReader />
      </HostedAccountsProvider>,
    );
    expect(await screen.findByText("Reporting available")).toBeInTheDocument();
  });

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
      accounts: [create(AccountSchema, { accountId, depth: 1 })],
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
            <Route path={`/accounts/${accountId}/overview`} element={<div>Customer workspace</div>} />
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
  it("keeps a mounted host workspace when a background directory refresh fails", async () => {
    vi.spyOn(hostedAccountsClient, "listAccounts")
      .mockResolvedValueOnce(create(ListAccountsResponseSchema, { accounts: [], hostAccess: true }))
      .mockRejectedValue(new ConnectError("Unavailable", Code.Unavailable));
    const mountHost = vi.fn();
    const HostPage = () => {
      const directory = useHostedAccounts()!;
      useEffect(() => mountHost(), []);
      return <div>{directory.error ? "Host fleet (stale directory)" : "Host fleet"}</div>;
    };
    login(33);
    render(
      <MemoryRouter>
        <HostedAccountsProvider>
          <HostWorkspaceGate>
            <HostPage />
          </HostWorkspaceGate>
        </HostedAccountsProvider>
      </MemoryRouter>,
    );
    await screen.findByText("Host fleet");
    act(() => window.dispatchEvent(new Event("focus")));
    await screen.findByText("Host fleet (stale directory)");
    expect(mountHost).toHaveBeenCalledTimes(1);
  });
  it("mounts Fleet for a site-only operator without org-scoped permissions or account memberships", async () => {
    vi.spyOn(hostedAccountsClient, "listAccounts").mockResolvedValue(
      create(ListAccountsResponseSchema, {
        accounts: [],
        hostAccess: true,
      }),
    );
    login(31);
    render(
      <MemoryRouter initialEntries={["/sites/site-one"]}>
        <HostedAccountsProvider>
          <Routes>
            <Route
              path="/sites/site-one"
              element={
                <HostWorkspaceGate>
                  <div>Authorized site details</div>
                </HostWorkspaceGate>
              }
            />
            <Route path="/accounts" element={<div>Account directory</div>} />
          </Routes>
        </HostedAccountsProvider>
      </MemoryRouter>,
    );
    expect(await screen.findByText("Authorized site details")).toBeInTheDocument();
    expect(useFleetStore.getState().auth.permissions).toEqual([]);
    expect(screen.queryByText("Account directory")).not.toBeInTheDocument();
  });
  it("keeps a login without Fleet authority out of host providers", async () => {
    vi.spyOn(hostedAccountsClient, "listAccounts").mockResolvedValue(
      create(ListAccountsResponseSchema, { accounts: [], hostAccess: false }),
    );
    const mountHost = vi.fn();
    const HostPage = () => {
      mountHost();
      return <div>Host data</div>;
    };
    login(32);
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
            <Route path="/accounts" element={<div>Account directory</div>} />
          </Routes>
        </HostedAccountsProvider>
      </MemoryRouter>,
    );
    expect(await screen.findByText("Account directory")).toBeInTheDocument();
    expect(mountHost).not.toHaveBeenCalled();
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
