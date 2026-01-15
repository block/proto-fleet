import { fireEvent, render, waitFor } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import PoolSelectionPage from "./PoolSelectionPage";
import { PoolSchema } from "@/protoFleet/api/generated/pools/v1/pools_pb";

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

vi.mock("@/protoFleet/api/usePools", () => ({
  default: () => ({
    pools: mockPools,
    miningPools: mockPools.map((pool) => ({
      poolId: pool.poolId.toString(),
      name: pool.poolName,
      poolUrl: pool.url,
      username: pool.username,
    })),
    validatePool: vi.fn(({ onSuccess }) => {
      onSuccess?.();
    }),
    createPool: vi.fn(),
    updatePool: vi.fn(),
    deletePool: vi.fn(),
    validatePoolPending: false,
  }),
}));

describe("Pool selection page", () => {
  const numberOfMiners = 5;
  const deviceIdentifiers = Array.from({ length: numberOfMiners }, (_, i) => `device-${i}`);

  const onCancel = vi.fn();
  const onAssignPools = vi.fn().mockResolvedValue(undefined);

  const defaultPoolTitle = "Default pool";
  const backupPool1Title = "Backup pool #1";
  const backupPool2Title = "Backup pool #2";

  test("renders page with default and backup pools section", () => {
    const { getByText } = render(
      <PoolSelectionPage deviceIdentifiers={deviceIdentifiers} onAssignPools={onAssignPools} onDismiss={onCancel} />,
    );

    expect(getByText("Assign pools")).toBeInTheDocument();
    expect(getByText(defaultPoolTitle)).toBeInTheDocument();
    expect(getByText(backupPool1Title)).toBeInTheDocument();
    expect(getByText(backupPool2Title)).toBeInTheDocument();
  });

  test("renders correct number of miners in button text", () => {
    const { getByText } = render(
      <PoolSelectionPage deviceIdentifiers={deviceIdentifiers} onAssignPools={onAssignPools} onDismiss={onCancel} />,
    );

    expect(getByText(`Assign to ${numberOfMiners} miners`)).toBeInTheDocument();
  });

  test("disables assign button when no default pool is selected", async () => {
    const { getByText } = render(
      <PoolSelectionPage deviceIdentifiers={deviceIdentifiers} onAssignPools={onAssignPools} onDismiss={onCancel} />,
    );

    const assignButton = getByText(`Assign to ${numberOfMiners} miners`).closest("button");
    expect(assignButton).toBeDisabled();
  });

  test("calls onCancel when close button clicked without changes", async () => {
    const { getAllByTestId } = render(
      <PoolSelectionPage deviceIdentifiers={deviceIdentifiers} onAssignPools={onAssignPools} onDismiss={onCancel} />,
    );

    const closeModalButton = getAllByTestId("header-icon-button")[0];
    fireEvent.click(closeModalButton);
    await waitFor(() => {
      expect(onCancel).toHaveBeenCalled();
    });
  });

  test("calls onCancel when close button clicked after selecting default pool", async () => {
    const { getByText, getAllByText, getAllByTestId } = render(
      <PoolSelectionPage deviceIdentifiers={deviceIdentifiers} onAssignPools={onAssignPools} onDismiss={onCancel} />,
    );

    expect(getByText(`Assign to ${numberOfMiners} miners`)).toBeInTheDocument();

    // Click the "Add pool" button for the default pool (first one)
    const addPoolButtons = getAllByText("Add pool");
    fireEvent.click(addPoolButtons[0]);

    // Wait for modal to open
    await waitFor(() => {
      expect(getByText("Select pool")).toBeInTheDocument();
    });

    // Select a pool from the modal by clicking on the pool row
    const poolRow = getByText("Client pool A1");
    fireEvent.click(poolRow);

    // Click Save button in the modal
    const saveButtons = getAllByText("Save");
    const modalSaveButton = saveButtons.find((btn) => btn.closest("button")) as HTMLElement;
    fireEvent.click(modalSaveButton);

    // Wait for validation to complete and modal to close (800ms minimum delay)
    await waitFor(
      () => {
        expect(() => getByText("Select pool")).toThrow();
      },
      { timeout: 2000 },
    );

    // Wait for the "Testing connection" phase to complete and "Update" button to appear
    await waitFor(
      () => {
        expect(getAllByText("Update").length).toBeGreaterThan(0);
      },
      { timeout: 2000 },
    );

    // Close the page
    const closeModalButton = getAllByTestId("header-icon-button")[0];
    fireEvent.click(closeModalButton);

    await waitFor(() => {
      expect(onCancel).toHaveBeenCalled();
    });
  });

  test("disables backup pool buttons when no default pool is selected", async () => {
    const { getByTestId } = render(
      <PoolSelectionPage deviceIdentifiers={deviceIdentifiers} onAssignPools={onAssignPools} onDismiss={onCancel} />,
    );

    const backupPool1 = getByTestId("backup-pool-1");
    const backupPool2 = getByTestId("backup-pool-2");

    expect(backupPool1).toHaveAttribute("aria-disabled", "true");
    expect(backupPool2).toHaveAttribute("aria-disabled", "true");

    expect(backupPool1.querySelector("button")).toBeDisabled();
    expect(backupPool2.querySelector("button")).toBeDisabled();
  });

  test("enables backup pool #1 after selecting default pool, but keeps #2 disabled", async () => {
    const { getByText, getAllByText, getByTestId } = render(
      <PoolSelectionPage deviceIdentifiers={deviceIdentifiers} onAssignPools={onAssignPools} onDismiss={onCancel} />,
    );

    const addPoolButtons = getAllByText("Add pool");
    fireEvent.click(addPoolButtons[0]);

    await waitFor(() => {
      expect(getByText("Select pool")).toBeInTheDocument();
    });

    fireEvent.click(getByText("Client pool A1"));
    fireEvent.click(getAllByText("Save").find((btn) => btn.closest("button")) as HTMLElement);

    await waitFor(
      () => {
        expect(getAllByText("Update").length).toBeGreaterThan(0);
      },
      { timeout: 2000 },
    );

    const backupPool1 = getByTestId("backup-pool-1");
    expect(backupPool1).toHaveAttribute("aria-disabled", "false");
    expect(backupPool1.querySelector("button")).not.toBeDisabled();

    const backupPool2 = getByTestId("backup-pool-2");
    expect(backupPool2).toHaveAttribute("aria-disabled", "true");
  });

  test("enables backup pool #2 after selecting backup pool #1", async () => {
    const { getByText, getAllByText, getByTestId, queryAllByText } = render(
      <PoolSelectionPage deviceIdentifiers={deviceIdentifiers} onAssignPools={onAssignPools} onDismiss={onCancel} />,
    );

    let addPoolButtons = getAllByText("Add pool");
    fireEvent.click(addPoolButtons[0]);

    await waitFor(() => {
      expect(getByText("Select pool")).toBeInTheDocument();
    });

    fireEvent.click(getByText("Client pool A1"));
    fireEvent.click(getAllByText("Save").find((btn) => btn.closest("button")) as HTMLElement);

    await waitFor(
      () => {
        expect(getAllByText("Update").length).toBeGreaterThan(0);
      },
      { timeout: 2000 },
    );

    addPoolButtons = getAllByText("Add pool");
    fireEvent.click(addPoolButtons[0]);

    await waitFor(() => {
      expect(queryAllByText("Select pool").length).toBeGreaterThan(0);
    });

    fireEvent.click(getByTestId("pool-row-Client pool A2"));
    fireEvent.click(getAllByText("Save").find((btn) => btn.closest("button")) as HTMLElement);

    await waitFor(
      () => {
        expect(getAllByText("Update").length).toBe(2);
      },
      { timeout: 2000 },
    );

    const backupPool2 = getByTestId("backup-pool-2");
    expect(backupPool2).toHaveAttribute("aria-disabled", "false");
    expect(backupPool2.querySelector("button")).not.toBeDisabled();
  });

  test("shows already selected pools as greyed out when slot is empty (swap only allowed when both have pools)", async () => {
    const { getAllByText, getByText, queryAllByText, getByTestId } = render(
      <PoolSelectionPage deviceIdentifiers={deviceIdentifiers} onAssignPools={onAssignPools} onDismiss={onCancel} />,
    );

    const addPoolButtons = getAllByText("Add pool");
    fireEvent.click(addPoolButtons[0]);

    await waitFor(() => {
      expect(getByText("Select pool")).toBeInTheDocument();
    });

    fireEvent.click(getByText("Client pool A1"));
    fireEvent.click(getAllByText("Save").find((btn) => btn.closest("button")) as HTMLElement);

    await waitFor(
      () => {
        expect(getAllByText("Update").length).toBeGreaterThan(0);
      },
      { timeout: 2000 },
    );

    // Open backup #1 modal (which is currently empty)
    fireEvent.click(getAllByText("Add pool")[0]);

    await waitFor(() => {
      expect(queryAllByText("Select pool").length).toBeGreaterThan(0);
    });

    // Pool A1 (assigned to Default) should show label but be disabled
    // because backup #1 is empty and swap would clear the default
    expect(queryAllByText("Client pool A1").length).toBe(2);
    expect(getByText("Default")).toBeInTheDocument();

    const pool1Row = getByTestId("pool-row-Client pool A1");
    expect(pool1Row).toHaveAttribute("aria-disabled", "true");
    expect(pool1Row.querySelector('input[type="radio"]')).toBeDisabled();
  });

  test("swaps backup pool with default when same pool is selected as default", async () => {
    const { getAllByText, getByText, getByTestId, queryAllByText } = render(
      <PoolSelectionPage deviceIdentifiers={deviceIdentifiers} onAssignPools={onAssignPools} onDismiss={onCancel} />,
    );

    let addPoolButtons = getAllByText("Add pool");
    fireEvent.click(addPoolButtons[0]);

    await waitFor(() => {
      expect(getByText("Select pool")).toBeInTheDocument();
    });

    // Select Client pool A1 as default
    fireEvent.click(getByTestId("pool-row-Client pool A1"));
    fireEvent.click(getAllByText("Save").find((btn) => btn.closest("button")) as HTMLElement);

    await waitFor(
      () => {
        expect(getAllByText("Update").length).toBeGreaterThan(0);
      },
      { timeout: 2000 },
    );

    // Select Client pool A2 as backup #1
    addPoolButtons = getAllByText("Add pool");
    fireEvent.click(addPoolButtons[0]);

    await waitFor(() => {
      expect(queryAllByText("Select pool").length).toBeGreaterThan(0);
    });

    fireEvent.click(getByTestId("pool-row-Client pool A2"));
    fireEvent.click(getAllByText("Save").find((btn) => btn.closest("button")) as HTMLElement);

    await waitFor(
      () => {
        expect(getAllByText("Update").length).toBeGreaterThan(1);
      },
      { timeout: 2000 },
    );

    // Now change default to Client pool A2 (which is currently backup #1)
    // This should swap the pools
    fireEvent.click(getByTestId("default-pool").querySelector("button")!);

    await waitFor(() => {
      expect(queryAllByText("Select pool").length).toBeGreaterThan(0);
    });

    fireEvent.click(getByTestId("pool-row-Client pool A2"));
    fireEvent.click(getAllByText("Save").find((btn) => btn.closest("button")) as HTMLElement);

    // Wait for the swap to complete - default pool should now show A2
    await waitFor(
      () => {
        const defaultPoolCard = getByTestId("default-pool");
        expect(defaultPoolCard).toHaveTextContent("Client pool A2");
      },
      { timeout: 3000 },
    );

    // Verify backup #1 shows A1 (the old default, now swapped)
    await waitFor(
      () => {
        const backupPool1Card = getByTestId("backup-pool-1");
        expect(backupPool1Card).toHaveTextContent("Client pool A1");
      },
      { timeout: 3000 },
    );
  });
});
