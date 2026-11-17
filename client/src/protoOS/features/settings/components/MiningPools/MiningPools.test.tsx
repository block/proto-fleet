import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";
import MiningPools from "./MiningPools";
import type { ContainerPoolConfiguration } from "@/protoOS/api/adapters/poolConfiguration";

const state = vi.hoisted(() => ({
  fleet: false,
  error: undefined as string | undefined,
  pools: [{ id: "pool:42", priority: 0, name: "Primary", url: "stratum+tcp://pool.example:3333", username: "worker1" }],
  adapter: {
    getContainerPoolConfiguration: vi.fn(),
    getMiningGroupPoolSettings: vi.fn(),
    getPoolSettings: vi.fn(),
    savePoolSettings: vi.fn(),
    deletePoolSettings: vi.fn(),
    reorderPoolSettings: vi.fn(),
  },
}));
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: () => ({ isFleetHosted: state.fleet, adapter: state.adapter }),
}));
vi.mock("@/protoOS/store", () => ({ useDeviceType: () => "container" }));
vi.mock("@/protoOS/api", () => ({
  usePoolsInfo: () => ({
    data: state.pools,
    pending: false,
    error: state.error,
  }),
  useCreatePools: () => ({ createPools: vi.fn() }),
  useEditPool: () => ({ editPool: vi.fn() }),
}));

const containerConfiguration: ContainerPoolConfiguration = {
  mode: "container",
  suspended: false,
  groups: [],
  unassignedBoards: [],
  locationsAvailable: true,
  pendingOperations: [],
  inventoryIssues: [],
};

beforeEach(() => {
  vi.clearAllMocks();
  state.fleet = false;
  state.error = undefined;
  state.adapter.getContainerPoolConfiguration.mockResolvedValue(containerConfiguration);
  state.adapter.getPoolSettings.mockResolvedValue(
    state.pools.map((pool) => ({ ...pool, id: "42", password: "", enabled: true })),
  );
  state.adapter.savePoolSettings.mockResolvedValue(undefined);
});

test("mining groups show scoped usernames and assignments without container editing", async () => {
  state.adapter.getContainerPoolConfiguration.mockResolvedValue({
    ...containerConfiguration,
    mode: "groups",
    groups: [
      {
        id: "client-a",
        name: "Client A",
        revision: "7",
        boards: [{ mac: "aa:bb:cc:00:00:01", location: "Tank 1 · Module 2 · Board 3" }],
      },
    ],
    pendingOperations: [{ id: "op-1", kind: "transfer", phase: "stopping", boards: ["aa:bb:cc:00:00:02"] }],
    inventoryIssues: [{ mac: "aa:bb:cc:00:00:02", reason: "feed mapping unavailable" }],
  });
  state.adapter.getMiningGroupPoolSettings.mockResolvedValue([
    {
      id: "1",
      name: "Client pool",
      url: "stratum+tcp://client:3333",
      username: "account.region.board-01",
      priority: 0,
      enabled: false,
    },
  ]);
  render(<MiningPools />);
  expect(await screen.findByText("Client A")).toBeInTheDocument();
  expect(screen.getByText("transfer · stopping")).toBeInTheDocument();
  expect(screen.getByText(/feed mapping unavailable/)).toBeInTheDocument();
  expect(screen.queryByText("Update")).not.toBeInTheDocument();
  expect(screen.queryByText("Add pool")).not.toBeInTheDocument();
  expect(state.adapter.getPoolSettings).not.toHaveBeenCalled();
  expect(state.adapter.getMiningGroupPoolSettings).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "View Client A hashboards" }));
  expect(screen.getByRole("dialog", { name: "Client A hashboards" })).toBeInTheDocument();
  expect(screen.getByText("Tank 1 · Module 2 · Board 3")).toBeInTheDocument();
  expect(state.adapter.getMiningGroupPoolSettings).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Close dialog" }));
  fireEvent.click(screen.getByRole("button", { name: "View Client A pools" }));
  expect(await screen.findByText("account.region.board-01")).toBeInTheDocument();
  expect(screen.getByText("Disabled · Priority 1")).toBeInTheDocument();
  expect(screen.queryByText("Tank 1 · Module 2 · Board 3")).not.toBeInTheDocument();
  expect(state.adapter.getMiningGroupPoolSettings).toHaveBeenCalledWith(
    "client-a",
    "7",
    expect.objectContaining({ signal: expect.any(AbortSignal) }),
  );
});

test("unknown mode blocks editing even when legacy pools could be read", async () => {
  state.adapter.getContainerPoolConfiguration.mockRejectedValue(new Error("offline"));
  render(<MiningPools />);
  expect(await screen.findByRole("alert")).toHaveTextContent("confirm mining-group mode");
  expect(screen.queryByText("Update")).not.toBeInTheDocument();
  expect(screen.queryByText("Add pool")).not.toBeInTheDocument();
});

test.each(["FAILED", "COMPLETE", "QUIESCING"])(
  "container mode shows %s operation details and blocks only unfinished work",
  async (phase) => {
    state.adapter.getContainerPoolConfiguration.mockResolvedValue({
      ...containerConfiguration,
      pendingOperations: [{ id: "op", kind: "transfer", phase, error: "Board needs inspection", boards: [] }],
    });
    render(<MiningPools />);
    expect(await screen.findByText("Board needs inspection")).toBeInTheDocument();
    if (phase === "QUIESCING") expect(screen.queryByText("Update")).not.toBeInTheDocument();
    else expect(screen.getByText("Update")).toBeInTheDocument();
  },
);

test("an authentication failure while reading offers sign-in guidance without retrying", async () => {
  state.adapter.getContainerPoolConfiguration.mockRejectedValue({ status: 401, error: { code: 16 } });
  render(<MiningPools />);
  expect(await screen.findByRole("alert")).toHaveTextContent("Sign in to the controller");
  expect(screen.queryByText("Add pool")).not.toBeInTheDocument();
  expect(state.adapter.getContainerPoolConfiguration).toHaveBeenCalledTimes(1);
});

test.each([false, true])(
  "a partial write keeps its warning through authentication loss (read also fails: %s)",
  async (readFails) => {
    state.adapter.savePoolSettings.mockRejectedValue(
      Object.assign(new Error("Some pool priorities were saved. The final order is not confirmed."), {
        status: 401,
        error: { code: 16 },
        writeOutcome: "unknown",
      }),
    );
    render(<MiningPools />);
    fireEvent.click(await screen.findByText("Update"));
    if (readFails) state.adapter.getContainerPoolConfiguration.mockRejectedValue({ status: 401, error: { code: 16 } });
    fireEvent.click(screen.getByText("Save"));
    expect(await screen.findByRole("alert")).toHaveTextContent("Some pool priorities were saved");
    expect(screen.getByRole("alert")).toHaveTextContent("Sign in to the controller");
    expect(screen.getByRole("alert")).toHaveTextContent("not retried");
    expect(state.adapter.savePoolSettings).toHaveBeenCalledTimes(1);
    expect(state.adapter.getContainerPoolConfiguration).toHaveBeenCalledTimes(2);
  },
);

test.each([
  { suspended: true, pendingOperations: [] },
  { suspended: false, pendingOperations: [{ id: "op", kind: "deactivate", phase: "stopping", boards: [] }] },
])("suspended or changing mode keeps saved container pools read-only: %j", async (mode) => {
  state.adapter.getContainerPoolConfiguration.mockResolvedValue({
    ...containerConfiguration,
    ...mode,
  });
  render(<MiningPools />);
  expect(await screen.findByText("Primary")).toBeInTheDocument();
  expect(screen.getByRole("status")).toHaveTextContent("Container-wide pool editing is unavailable");
  expect(screen.queryByText("Update")).not.toBeInTheDocument();
});

test("a mode change discovered on save replaces the legacy editor with group inspection", async () => {
  state.adapter.savePoolSettings.mockRejectedValue(
    Object.assign(new Error("Mining groups are enabled."), { writeOutcome: "refused" }),
  );
  render(<MiningPools />);
  fireEvent.click(await screen.findByText("Update"));
  state.adapter.getContainerPoolConfiguration.mockResolvedValue({
    ...containerConfiguration,
    mode: "groups",
  });
  fireEvent.click(screen.getByText("Save"));
  expect(await screen.findByRole("alert")).toHaveTextContent("Mining groups are enabled");
  expect(screen.getByText("No mining groups reported.")).toBeInTheDocument();
  expect(screen.queryByText("Update")).not.toBeInTheDocument();
});

test("direct CT1 pools reuse editing controls and save against the stable pool ID", async () => {
  render(<MiningPools />);
  expect(await screen.findByText("Primary")).toBeInTheDocument();
  expect(screen.getByText("worker1")).toBeInTheDocument();
  expect(screen.getByText(/Add up to 3 pools in order of priority/)).toHaveTextContent("automatically");
  fireEvent.click(screen.getByText("Update"));
  fireEvent.change(screen.getByTestId("pool-name-0-input"), { target: { value: "Renamed" } });
  fireEvent.click(screen.getByText("Save"));
  await waitFor(() =>
    expect(state.adapter.savePoolSettings).toHaveBeenCalledWith(
      expect.objectContaining({ name: "Renamed", password: "" }),
      "42",
    ),
  );
  expect(screen.queryByText(/managed from Fleet/i)).not.toBeInTheDocument();
  expect(screen.queryByText("Test connection")).not.toBeInTheDocument();
});

test("Fleet-hosted CT1 pools retain the Fleet management explanation", () => {
  state.fleet = true;
  render(<MiningPools />);
  expect(screen.getByText(/managed from Fleet/i)).toBeInTheDocument();
  expect(screen.queryByText("Update")).not.toBeInTheDocument();
  expect(state.adapter.getPoolSettings).not.toHaveBeenCalled();
});

test("failed pool reads show an error instead of an empty configuration", () => {
  state.error = "unreachable";
  state.fleet = true;
  render(<MiningPools />);
  expect(screen.getByText("Could not load pools")).toBeInTheDocument();
  expect(screen.queryByText("No pools configured.")).not.toBeInTheDocument();
});

test("a failed read offers retry without enabling pool creation", async () => {
  state.adapter.getPoolSettings.mockRejectedValue(new Error("offline"));
  render(<MiningPools />);
  expect(await screen.findByRole("alert")).toHaveTextContent("Could not reload pools");
  expect(screen.queryByText("Add pool")).not.toBeInTheDocument();
  state.adapter.getPoolSettings.mockResolvedValue([]);
  fireEvent.click(screen.getByText("Reload pools"));
  expect(await screen.findByText("Add pool")).toBeInTheDocument();
});

test.each([false, true])("leaving pools cancels the pending configuration read (reload: %s)", async (reload) => {
  const view = reload ? render(<MiningPools />) : undefined;
  if (reload) await screen.findByText("Primary");

  let signal: AbortSignal | undefined;
  state.adapter.getContainerPoolConfiguration.mockImplementationOnce(
    ({ signal: readSignal }: { signal: AbortSignal }) => {
      signal = readSignal;
      return new Promise((_, reject) => {
        readSignal.addEventListener("abort", () => reject(readSignal.reason), { once: true });
      });
    },
  );
  const { unmount } = view ?? render(<MiningPools />);
  if (reload) fireEvent.click(screen.getByText("Reload pools"));

  expect(signal?.aborted).toBe(false);
  await act(async () => unmount());
  expect(signal?.aborted).toBe(true);
  expect(state.adapter.getPoolSettings).toHaveBeenCalledTimes(reload ? 1 : 0);
});

test("a failed write reconciles persisted state without retrying the mutation", async () => {
  state.adapter.savePoolSettings.mockRejectedValue(new Error("live reload failed"));
  render(<MiningPools />);
  fireEvent.click(await screen.findByText("Update"));
  fireEvent.click(screen.getByText("Save"));
  expect(await screen.findByRole("alert")).toHaveTextContent("Review the reloaded configuration");
  expect(state.adapter.savePoolSettings).toHaveBeenCalledTimes(1);
  expect(state.adapter.getPoolSettings).toHaveBeenCalledTimes(2);
});

test("creating a pool appends after the highest reported priority", async () => {
  state.adapter.getPoolSettings.mockResolvedValue([
    { ...state.pools[0], id: "42", priority: 7, password: "", enabled: true },
  ]);
  render(<MiningPools />);
  fireEvent.click(await screen.findByText("Add another pool"));
  fireEvent.change(screen.getByTestId("pool-name-1-input"), { target: { value: "Backup" } });
  fireEvent.change(screen.getByTestId("url-1-input"), { target: { value: "stratum+tcp://backup.example:3333" } });
  fireEvent.change(screen.getByTestId("username-1-input"), { target: { value: "worker2" } });
  fireEvent.click(screen.getByText("Save"));
  await waitFor(() =>
    expect(state.adapter.savePoolSettings).toHaveBeenCalledWith(
      expect.objectContaining({ name: "Backup", priority: 8 }),
      undefined,
    ),
  );
});

test("deleting one pool uses its ID without replacing the remaining credentials", async () => {
  state.adapter.getPoolSettings.mockResolvedValue(
    ["42", "91"].map((id, priority) => ({ ...state.pools[0], id, priority, password: "", enabled: true })),
  );
  render(<MiningPools />);
  fireEvent.click((await screen.findAllByText("Update"))[1]);
  fireEvent.click(screen.getByText("Delete"));
  await waitFor(() => expect(state.adapter.deletePoolSettings).toHaveBeenCalledWith("91"));
  expect(state.adapter.savePoolSettings).not.toHaveBeenCalled();
  expect(state.adapter.reorderPoolSettings).not.toHaveBeenCalled();
});

test.each([
  "datum://gateway.example:23334",
  "tcp://pool.example:3333",
  "ssl://pool.example:3334",
  "stratum+tcp://pool.example:3333",
  "stratum+ssl://pool.example:3334",
  "pool.example:3333",
])("an existing CT1 pool using %s can be renamed", async (url) => {
  state.adapter.getPoolSettings.mockResolvedValue([{ ...state.pools[0], id: "42", url, password: "", enabled: true }]);
  render(<MiningPools />);
  fireEvent.click(await screen.findByText("Update"));
  fireEvent.change(screen.getByTestId("pool-name-0-input"), { target: { value: "Renamed" } });
  fireEvent.click(screen.getByText("Save"));
  await waitFor(() =>
    expect(state.adapter.savePoolSettings).toHaveBeenCalledWith(
      expect.objectContaining({ name: "Renamed", url }),
      "42",
    ),
  );
});

test.each(["stratum+ws://pool.example:3333", "stratum2+tcp://pool.example:3336/key", "datum://"])(
  "CT1 URL validation blocks %s and clears when corrected",
  async (url) => {
    render(<MiningPools />);
    fireEvent.click(await screen.findByText("Update"));
    const input = screen.getByTestId("url-0-input");
    fireEvent.change(input, { target: { value: url } });
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(state.adapter.savePoolSettings).not.toHaveBeenCalled();

    fireEvent.change(input, { target: { value: "datum://gateway.example:23334" } });
    expect(screen.getByRole("button", { name: "Save" })).toBeEnabled();
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(state.adapter.savePoolSettings).toHaveBeenCalledWith(
        expect.objectContaining({ url: "datum://gateway.example:23334" }),
        "42",
      ),
    );
  },
);
