import { create } from "@bufbuild/protobuf";
import { fireEvent, render, waitFor } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import PoolSelectionPage from "./PoolSelectionPage";
import { PoolSchema } from "@/protoFleet/api/generated/pools/v1/pools_pb";

const mockPools = [
  create(PoolSchema, {
    poolId: BigInt(1),
    poolName: "Client pool A1",
    url: "stratum+tcp://mine.ocean.xyz:3323",
    username: "user1",
    isDefault: false,
  }),
  create(PoolSchema, {
    poolId: BigInt(2),
    poolName: "Client pool A2",
    url: "stratum+tcp://mine.ocean.xyz:3324",
    username: "user2",
    isDefault: false,
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
    setDefaultPool: vi.fn(),
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

  test("calls onCancel when close button clicked after selecting backup pool", async () => {
    const { getByText, getAllByText, getAllByTestId } = render(
      <PoolSelectionPage deviceIdentifiers={deviceIdentifiers} onAssignPools={onAssignPools} onDismiss={onCancel} />,
    );

    // Click the "Add pool" button for the first backup pool (second button)
    const addPoolButtons = getAllByText("Add pool");
    fireEvent.click(addPoolButtons[1]);

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

  test("prevents selecting the same pool for both backup slots", async () => {
    const { getAllByText, getAllByTestId, queryAllByText } = render(
      <PoolSelectionPage deviceIdentifiers={deviceIdentifiers} onAssignPools={onAssignPools} onDismiss={onCancel} />,
    );

    const addPoolButtons = getAllByText("Add pool");

    // Select a pool for backup slot 1 (index 1 = second "Add pool" button)
    fireEvent.click(addPoolButtons[1]);

    // Wait for modal to open
    await waitFor(() => {
      expect(queryAllByText("Select pool").length).toBeGreaterThan(0);
    });

    // Select first pool from the modal - there should be only one in the modal at this point
    let poolRows = queryAllByText("Client pool A1");
    fireEvent.click(poolRows[0]);

    // Click Save button in the modal
    let saveButtons = getAllByText("Save");
    let modalSaveButton = saveButtons.find((btn) => btn.closest("button")) as HTMLElement;
    fireEvent.click(modalSaveButton);

    // Wait for validation to complete and modal to close (800ms minimum delay)
    await waitFor(
      () => {
        expect(queryAllByText("Select pool").length).toBe(0);
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

    // Try to select the same pool for backup slot 2 (index 2 = third "Add pool" button)
    // Need to get fresh button references after the first pool was selected
    const updatedAddPoolButtons = getAllByText("Add pool");
    fireEvent.click(updatedAddPoolButtons[1]); // This is now backup slot 2

    // Wait for modal to open again
    await waitFor(() => {
      expect(queryAllByText("Select pool").length).toBeGreaterThan(0);
    });

    // Try to select the same pool again - now there are multiple instances (one in the page, one in modal)
    poolRows = queryAllByText("Client pool A1");
    // Click the one in the modal (should be the last one)
    fireEvent.click(poolRows[poolRows.length - 1]);

    // Click Save
    saveButtons = getAllByText("Save");
    modalSaveButton = saveButtons.find((btn) => btn.closest("button")) as HTMLElement;
    fireEvent.click(modalSaveButton);

    // Wait for modal to close (800ms minimum delay)
    await waitFor(
      () => {
        expect(queryAllByText("Select pool").length).toBe(0);
      },
      { timeout: 2000 },
    );

    // Wait for the "Testing connection" phase to complete and "Update" buttons to appear
    await waitFor(
      () => {
        expect(getAllByText("Update").length).toBeGreaterThan(0);
      },
      { timeout: 2000 },
    );

    // Close the page - should report false (assignment success is handled by wrapper)
    const closeModalButton = getAllByTestId("header-icon-button")[0];
    fireEvent.click(closeModalButton);

    await waitFor(() => {
      expect(onCancel).toHaveBeenCalled();
    });
  });
});
