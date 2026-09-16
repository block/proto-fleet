import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";
import MiningPools from "./MiningPools";

const state = vi.hoisted(() => ({
  fleet: false,
  error: undefined as string | undefined,
  pools: [{ id: "pool:42", priority: 0, name: "Primary", url: "stratum+tcp://pool.example:3333", username: "worker1" }],
  adapter: {
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

beforeEach(() => {
  vi.clearAllMocks();
  state.fleet = false;
  state.error = undefined;
  state.adapter.getPoolSettings.mockResolvedValue(
    state.pools.map((pool) => ({ ...pool, id: "42", password: "", enabled: true })),
  );
  state.adapter.savePoolSettings.mockResolvedValue(undefined);
});

test("direct CT1 pools reuse editing controls and save against the stable pool ID", async () => {
  render(<MiningPools />);
  expect(await screen.findByText("Primary")).toBeInTheDocument();
  expect(screen.getByText("worker1")).toBeInTheDocument();
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
