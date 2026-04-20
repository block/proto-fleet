import { fireEvent, render } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import PoolsList from ".";
import { PoolSchema } from "@/protoFleet/api/generated/pools/v1/pools_pb";
import usePools from "@/protoFleet/api/usePools";

vi.mock("@/protoFleet/api/usePools");

describe("Pools list", () => {
  const mockPools = [
    create(PoolSchema, {
      poolId: BigInt(1),
      poolName: "Client pool A1",
      url: "stratum+tcp://mine.ocean.xyz:3323",
      username: "user1",
    }),
    create(PoolSchema, {
      poolId: BigInt(2),
      poolName: "Client pool A2",
      url: "stratum+tcp://mine.ocean.xyz:3324",
      username: "user2",
    }),
  ];

  const onSelect = vi.fn();

  beforeEach(() => {
    vi.mocked(usePools).mockReturnValue({
      pools: mockPools,
      miningPools: mockPools.map((pool) => ({
        poolId: pool.poolId.toString(),
        name: pool.poolName,
        poolUrl: pool.url,
        username: pool.username,
      })),
      validatePool: vi.fn(),
      createPool: vi.fn(),
      updatePool: vi.fn(),
      deletePool: vi.fn(),
      validatePoolPending: false,
      isLoading: false,
    });
  });

  const defaultPoolTitle = "Default pool";
  const defaultPoolSubtitle = "Select one default pool";
  const backupPoolTitle = "Backup pool #1";
  const backupPoolSubtitle = "Optional";
  const addDefaultPoolLabel = "Add pool";
  const addBackupPoolLabel = "Add pool";

  test("renders pool card with default pool", () => {
    const { getByText, getByRole } = render(
      <PoolsList
        title={defaultPoolTitle}
        subtitle={defaultPoolSubtitle}
        onSelect={onSelect}
        createNewLabel={addDefaultPoolLabel}
      />,
    );

    expect(getByText(defaultPoolTitle)).toBeInTheDocument();
    if (defaultPoolSubtitle) {
      expect(getByText(defaultPoolSubtitle)).toBeInTheDocument();
    }
    expect(getByText(addDefaultPoolLabel)).toBeInTheDocument();
    expect(getByRole("button", { name: addDefaultPoolLabel })).toBeInTheDocument();
  });

  test("renders pool card with backup pool and number badge", () => {
    const { getByText, getByRole } = render(
      <PoolsList
        title={backupPoolTitle}
        subtitle={backupPoolSubtitle}
        onSelect={onSelect}
        createNewLabel={addBackupPoolLabel}
        poolNumber={1}
      />,
    );

    expect(getByText(backupPoolTitle)).toBeInTheDocument();
    expect(getByText(backupPoolSubtitle)).toBeInTheDocument();
    expect(getByText(addBackupPoolLabel)).toBeInTheDocument();
    expect(getByRole("button", { name: addBackupPoolLabel })).toBeInTheDocument();
    expect(getByText("1")).toBeInTheDocument();
  });

  test("opens pool selection modal when Add pool button is clicked", () => {
    const { getByRole, getByText } = render(
      <PoolsList
        title={defaultPoolTitle}
        subtitle={defaultPoolSubtitle}
        onSelect={onSelect}
        createNewLabel={addDefaultPoolLabel}
      />,
    );

    fireEvent.click(getByRole("button", { name: addDefaultPoolLabel }));
    expect(getByText("Select pool")).toBeInTheDocument();
  });

  test("disables Add pool button when disabled prop is true", () => {
    const { getByRole } = render(
      <PoolsList
        title={backupPoolTitle}
        subtitle={backupPoolSubtitle}
        onSelect={onSelect}
        createNewLabel={addBackupPoolLabel}
        poolNumber={1}
        disabled={true}
      />,
    );

    const addButton = getByRole("button", { name: addBackupPoolLabel });
    expect(addButton).toBeDisabled();
  });

  test("sets aria-disabled when disabled", () => {
    const { getByTestId } = render(
      <PoolsList
        title={backupPoolTitle}
        subtitle={backupPoolSubtitle}
        onSelect={onSelect}
        createNewLabel={addBackupPoolLabel}
        poolNumber={1}
        disabled={true}
        testId="backup-pool-1"
      />,
    );

    const poolCard = getByTestId("backup-pool-1");
    expect(poolCard).toHaveAttribute("aria-disabled", "true");
  });
});
