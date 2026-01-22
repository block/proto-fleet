import { fireEvent, render } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import PoolSelectionModal from "./PoolSelectionModal";
import { PoolSchema } from "@/protoFleet/api/generated/pools/v1/pools_pb";
import usePools from "@/protoFleet/api/usePools";

vi.mock("@/protoFleet/api/usePools");

describe("PoolSelectionModal", () => {
  const mockPools = [
    create(PoolSchema, {
      poolId: BigInt(1),
      poolName: "Ocean Pool",
      url: "stratum+tcp://mine.ocean.xyz:3334",
      username: "ocean_user",
    }),
    create(PoolSchema, {
      poolId: BigInt(2),
      poolName: "Braiins Pool",
      url: "stratum+tcp://stratum.braiins.com:3333",
      username: "braiins_user",
    }),
    create(PoolSchema, {
      poolId: BigInt(3),
      poolName: "Foundry USA",
      url: "stratum+tcp://stratum.foundryusapool.com:3333",
      username: "foundry_user",
    }),
  ];

  const mockValidatePool = vi.fn();
  const mockCreatePool = vi.fn();
  const onDismiss = vi.fn();
  const onSave = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(usePools).mockReturnValue({
      pools: mockPools,
      miningPools: mockPools.map((pool) => ({
        poolId: pool.poolId.toString(),
        name: pool.poolName,
        poolUrl: pool.url,
        username: pool.username,
      })),
      validatePool: mockValidatePool,
      createPool: mockCreatePool,
      updatePool: vi.fn(),
      deletePool: vi.fn(),
      validatePoolPending: false,
      isLoading: false,
    });
  });

  test("renders modal with pool list", () => {
    const { getByText } = render(<PoolSelectionModal onDismiss={onDismiss} onSave={onSave} />);

    expect(getByText("Select pool")).toBeInTheDocument();
    expect(getByText("Ocean Pool")).toBeInTheDocument();
    expect(getByText("Braiins Pool")).toBeInTheDocument();
    expect(getByText("Foundry USA")).toBeInTheDocument();
  });

  test("renders search input", () => {
    const { getByTestId } = render(<PoolSelectionModal onDismiss={onDismiss} onSave={onSave} />);

    const searchInput = getByTestId("pool-search-input");
    expect(searchInput).toBeInTheDocument();
  });

  test("autofocuses the search input on mount", () => {
    const { getByTestId } = render(<PoolSelectionModal onDismiss={onDismiss} onSave={onSave} />);

    const searchInput = getByTestId("pool-search-input");
    expect(searchInput).toHaveFocus();
  });

  test("filters pools by name", () => {
    const { getByTestId, getByText, queryByText } = render(
      <PoolSelectionModal onDismiss={onDismiss} onSave={onSave} />,
    );

    const searchInput = getByTestId("pool-search-input");
    fireEvent.change(searchInput, { target: { value: "ocean" } });

    expect(getByText("Ocean Pool")).toBeInTheDocument();
    expect(queryByText("Braiins Pool")).not.toBeInTheDocument();
    expect(queryByText("Foundry USA")).not.toBeInTheDocument();
  });

  test("filters pools by URL", () => {
    const { getByTestId, getByText, queryByText } = render(
      <PoolSelectionModal onDismiss={onDismiss} onSave={onSave} />,
    );

    const searchInput = getByTestId("pool-search-input");
    fireEvent.change(searchInput, { target: { value: "braiins.com" } });

    expect(queryByText("Ocean Pool")).not.toBeInTheDocument();
    expect(getByText("Braiins Pool")).toBeInTheDocument();
    expect(queryByText("Foundry USA")).not.toBeInTheDocument();
  });

  test("filters pools by username", () => {
    const { getByTestId, getByText, queryByText } = render(
      <PoolSelectionModal onDismiss={onDismiss} onSave={onSave} />,
    );

    const searchInput = getByTestId("pool-search-input");
    fireEvent.change(searchInput, { target: { value: "foundry_user" } });

    expect(queryByText("Ocean Pool")).not.toBeInTheDocument();
    expect(queryByText("Braiins Pool")).not.toBeInTheDocument();
    expect(getByText("Foundry USA")).toBeInTheDocument();
  });

  test("shows 'No pools found' when search returns no results", () => {
    const { getByTestId, getByText } = render(<PoolSelectionModal onDismiss={onDismiss} onSave={onSave} />);

    const searchInput = getByTestId("pool-search-input");
    fireEvent.change(searchInput, { target: { value: "nonexistent" } });

    expect(getByText("No pools found")).toBeInTheDocument();
  });

  test("selecting a pool and clicking Save calls onSave with pool ID", () => {
    const { getByText } = render(<PoolSelectionModal onDismiss={onDismiss} onSave={onSave} />);

    const poolRow = getByText("Ocean Pool");
    fireEvent.click(poolRow);

    const saveButton = getByText("Save");
    fireEvent.click(saveButton);

    expect(onSave).toHaveBeenCalledWith("1");
  });

  test("Save button is disabled when no pool is selected", () => {
    const { getByText } = render(<PoolSelectionModal onDismiss={onDismiss} onSave={onSave} />);

    const saveButton = getByText("Save").closest("button");
    expect(saveButton).toBeDisabled();
  });

  test("Save button is enabled when a pool is selected", () => {
    const { getByText } = render(<PoolSelectionModal onDismiss={onDismiss} onSave={onSave} />);

    const poolRow = getByText("Ocean Pool");
    fireEvent.click(poolRow);

    const saveButton = getByText("Save").closest("button");
    expect(saveButton).not.toBeDisabled();
  });

  test("clicking 'Add new pool' button opens PoolModal", () => {
    const { getByText } = render(<PoolSelectionModal onDismiss={onDismiss} onSave={onSave} />);

    const addNewPoolButton = getByText("Add new pool");
    fireEvent.click(addNewPoolButton);

    expect(getByText("Save")).toBeInTheDocument();
  });

  test("renders pool data in correct columns", () => {
    const { getByText } = render(<PoolSelectionModal onDismiss={onDismiss} onSave={onSave} />);

    // Check column headers
    expect(getByText("Name")).toBeInTheDocument();
    expect(getByText("URL")).toBeInTheDocument();
    expect(getByText("Username")).toBeInTheDocument();

    // Check pool data is displayed
    expect(getByText("Ocean Pool")).toBeInTheDocument();
    expect(getByText("stratum+tcp://mine.ocean.xyz:3334")).toBeInTheDocument();
    expect(getByText("ocean_user")).toBeInTheDocument();
  });

  test("search is case insensitive", () => {
    const { getByTestId, getByText, queryByText } = render(
      <PoolSelectionModal onDismiss={onDismiss} onSave={onSave} />,
    );

    const searchInput = getByTestId("pool-search-input");
    fireEvent.change(searchInput, { target: { value: "OCEAN" } });

    expect(getByText("Ocean Pool")).toBeInTheDocument();
    expect(queryByText("Braiins Pool")).not.toBeInTheDocument();
  });

  test("clearing search shows all pools again", () => {
    const { getByTestId, getByText } = render(<PoolSelectionModal onDismiss={onDismiss} onSave={onSave} />);

    const searchInput = getByTestId("pool-search-input");

    // First filter
    fireEvent.change(searchInput, { target: { value: "ocean" } });
    expect(getByText("Ocean Pool")).toBeInTheDocument();

    // Clear filter
    fireEvent.change(searchInput, { target: { value: "" } });
    expect(getByText("Ocean Pool")).toBeInTheDocument();
    expect(getByText("Braiins Pool")).toBeInTheDocument();
    expect(getByText("Foundry USA")).toBeInTheDocument();
  });
});
