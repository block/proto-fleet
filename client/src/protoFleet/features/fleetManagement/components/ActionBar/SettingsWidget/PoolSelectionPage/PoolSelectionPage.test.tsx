import { fireEvent, render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
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
  create(PoolSchema, {
    poolId: BigInt(3),
    poolName: "Client pool A3",
    url: "stratum+tcp://mine.ocean.xyz:3325",
    username: "user3",
  }),
];

const mockValidatePool = vi.fn(({ onSuccess, onFinally }) => {
  // Pass a fully-verified outcome so the page renders the success
  // (rather than reachable-but-unverified) callout — the tests below
  // assert against the green "Pool connection successful" state.
  onSuccess?.({
    reachable: true,
    credentialsVerified: true,
    mode: 1, // ValidationMode.SV1_AUTHENTICATE
  });
  onFinally?.();
});
const mockFetchPoolAssignments = vi.fn().mockResolvedValue([]);

vi.mock("@/protoFleet/api/usePools", () => ({
  default: () => ({
    pools: mockPools,
    miningPools: mockPools.map((pool) => ({
      poolId: pool.poolId.toString(),
      name: pool.poolName,
      poolUrl: pool.url,
      username: pool.username,
    })),
    validatePool: mockValidatePool,
    createPool: vi.fn(),
    updatePool: vi.fn(),
    deletePool: vi.fn(),
    validatePoolPending: false,
  }),
}));

vi.mock("@/protoFleet/api/useMinerPoolAssignments", () => ({
  default: () => ({
    fetchPoolAssignments: mockFetchPoolAssignments,
    isLoading: false,
  }),
}));

describe("Pool selection page", () => {
  const numberOfMiners = 5;
  const deviceIdentifiers = Array.from({ length: numberOfMiners }, (_, i) => `device-${i}`);

  const onCancel = vi.fn();
  const onAssignPools = vi.fn().mockResolvedValue(undefined);

  beforeEach(() => {
    vi.clearAllMocks();
  });

  test("renders page with Add pool button when no pools configured", () => {
    const { getByText, getByTestId } = render(
      <PoolSelectionPage deviceIdentifiers={deviceIdentifiers} onAssignPools={onAssignPools} onDismiss={onCancel} />,
    );

    expect(getByText("Assign pools")).toBeInTheDocument();
    expect(getByTestId("add-pool-button")).toBeInTheDocument();
    expect(getByText("Add pool")).toBeInTheDocument();
  });

  test("renders correct number of miners in button text", () => {
    const { getByText } = render(
      <PoolSelectionPage deviceIdentifiers={deviceIdentifiers} onAssignPools={onAssignPools} onDismiss={onCancel} />,
    );

    expect(getByText(`Assign to ${numberOfMiners} miners`)).toBeInTheDocument();
  });

  test("uses numberOfMiners override when provided (Select All scenario)", () => {
    // Simulates the "Select All" scenario where:
    // - deviceIdentifiers contains only 50 visible miners from pagination
    // - numberOfMiners is the actual total count (e.g., 297)
    const visibleDeviceIdentifiers = Array.from({ length: 50 }, (_, i) => `device-${i}`);
    const totalMinerCount = 297;

    const { getByText } = render(
      <PoolSelectionPage
        deviceIdentifiers={visibleDeviceIdentifiers}
        numberOfMiners={totalMinerCount}
        onAssignPools={onAssignPools}
        onDismiss={onCancel}
      />,
    );

    // Should show the override count (297), not the deviceIdentifiers length (50)
    expect(getByText(`Assign to ${totalMinerCount} miners`)).toBeInTheDocument();
  });

  test("disables assign button when no pools are configured", async () => {
    const { getByText } = render(
      <PoolSelectionPage deviceIdentifiers={deviceIdentifiers} onAssignPools={onAssignPools} onDismiss={onCancel} />,
    );

    const assignButton = getByText(`Assign to ${numberOfMiners} miners`).closest("button");
    expect(assignButton).toBeDisabled();
  });

  test("calls onCancel when close button clicked", async () => {
    const { getAllByTestId } = render(
      <PoolSelectionPage deviceIdentifiers={deviceIdentifiers} onAssignPools={onAssignPools} onDismiss={onCancel} />,
    );

    const closeModalButton = getAllByTestId("header-icon-button")[0];
    fireEvent.click(closeModalButton);
    await waitFor(() => {
      expect(onCancel).toHaveBeenCalled();
    });
  });

  test("does not handle Escape when page is hidden", () => {
    render(
      <PoolSelectionPage
        open={false}
        deviceIdentifiers={deviceIdentifiers}
        onAssignPools={onAssignPools}
        onDismiss={onCancel}
      />,
    );

    fireEvent.keyDown(document, { key: "Escape" });

    expect(onCancel).not.toHaveBeenCalled();
  });

  test("loads assignments only after page becomes visible", async () => {
    const singleDevice = ["device-1"];

    const { rerender } = render(
      <PoolSelectionPage
        open={false}
        deviceIdentifiers={singleDevice}
        onAssignPools={onAssignPools}
        onDismiss={onCancel}
      />,
    );

    expect(mockFetchPoolAssignments).not.toHaveBeenCalled();

    rerender(
      <PoolSelectionPage
        open={true}
        deviceIdentifiers={singleDevice}
        onAssignPools={onAssignPools}
        onDismiss={onCancel}
      />,
    );

    await waitFor(() => {
      expect(mockFetchPoolAssignments).toHaveBeenCalledWith("device-1");
    });
  });

  test("opens selection modal when Add pool button is clicked", async () => {
    const { getByText, getByTestId } = render(
      <PoolSelectionPage deviceIdentifiers={deviceIdentifiers} onAssignPools={onAssignPools} onDismiss={onCancel} />,
    );

    const addPoolButton = getByTestId("add-pool-button");
    fireEvent.click(addPoolButton);

    await waitFor(() => {
      expect(getByText("Select pool")).toBeInTheDocument();
    });
  });

  test("adds pool to list when selected from modal", async () => {
    const { getByText, getByTestId, getAllByText } = render(
      <PoolSelectionPage deviceIdentifiers={deviceIdentifiers} onAssignPools={onAssignPools} onDismiss={onCancel} />,
    );

    // Click Add pool button
    fireEvent.click(getByTestId("add-pool-button"));

    await waitFor(() => {
      expect(getByText("Select pool")).toBeInTheDocument();
    });

    // Select a pool from the modal
    fireEvent.click(getByText("Client pool A1"));

    // Click Save button
    const saveButton = getAllByText("Save").find((btn) => btn.closest("button")) as HTMLElement;
    fireEvent.click(saveButton);

    // Pool should be added to the list
    await waitFor(() => {
      expect(getByTestId("pool-row-0")).toBeInTheDocument();
    });

    // Should show "Add another pool" button since we can add more
    expect(getByTestId("add-another-pool-button")).toBeInTheDocument();
  });

  test("shows Update button for each configured pool", async () => {
    const { getByText, getByTestId, getAllByText } = render(
      <PoolSelectionPage deviceIdentifiers={deviceIdentifiers} onAssignPools={onAssignPools} onDismiss={onCancel} />,
    );

    // Add first pool
    fireEvent.click(getByTestId("add-pool-button"));
    await waitFor(() => expect(getByText("Select pool")).toBeInTheDocument());
    fireEvent.click(getByText("Client pool A1"));
    fireEvent.click(getAllByText("Save").find((btn) => btn.closest("button")) as HTMLElement);

    await waitFor(() => {
      expect(getByTestId("pool-row-0")).toBeInTheDocument();
    });

    // Add second pool
    fireEvent.click(getByTestId("add-another-pool-button"));
    await waitFor(() => expect(getByText("Select pool")).toBeInTheDocument());
    fireEvent.click(getByText("Client pool A2"));
    fireEvent.click(getAllByText("Save").find((btn) => btn.closest("button")) as HTMLElement);

    await waitFor(() => {
      expect(getByTestId("pool-row-1")).toBeInTheDocument();
    });

    // Both pools should have Update buttons
    expect(getAllByText("Update").length).toBe(2);
  });

  test("enables assign button after adding a pool", async () => {
    const { getByText, getByTestId, getAllByText } = render(
      <PoolSelectionPage deviceIdentifiers={deviceIdentifiers} onAssignPools={onAssignPools} onDismiss={onCancel} />,
    );

    // Assign button should be disabled initially
    const assignButton = getByText(`Assign to ${numberOfMiners} miners`).closest("button");
    expect(assignButton).toBeDisabled();

    // Add a pool
    fireEvent.click(getByTestId("add-pool-button"));
    await waitFor(() => expect(getByText("Select pool")).toBeInTheDocument());
    fireEvent.click(getByText("Client pool A1"));
    fireEvent.click(getAllByText("Save").find((btn) => btn.closest("button")) as HTMLElement);

    // Wait for pool to be added
    await waitFor(() => {
      expect(getByTestId("pool-row-0")).toBeInTheDocument();
    });

    // Assign button should be enabled now
    expect(assignButton).not.toBeDisabled();
  });

  test("hides Add another pool button when 3 pools are configured", async () => {
    const { getByText, getByTestId, getAllByText, queryByTestId } = render(
      <PoolSelectionPage deviceIdentifiers={deviceIdentifiers} onAssignPools={onAssignPools} onDismiss={onCancel} />,
    );

    // Add first pool
    fireEvent.click(getByTestId("add-pool-button"));
    await waitFor(() => expect(getByText("Select pool")).toBeInTheDocument());
    fireEvent.click(getByText("Client pool A1"));
    fireEvent.click(getAllByText("Save").find((btn) => btn.closest("button")) as HTMLElement);
    await waitFor(() => expect(getByTestId("pool-row-0")).toBeInTheDocument());

    // Add second pool
    fireEvent.click(getByTestId("add-another-pool-button"));
    await waitFor(() => expect(getByText("Select pool")).toBeInTheDocument());
    fireEvent.click(getByText("Client pool A2"));
    fireEvent.click(getAllByText("Save").find((btn) => btn.closest("button")) as HTMLElement);
    await waitFor(() => expect(getByTestId("pool-row-1")).toBeInTheDocument());

    // Add third pool
    fireEvent.click(getByTestId("add-another-pool-button"));
    await waitFor(() => expect(getByText("Select pool")).toBeInTheDocument());
    fireEvent.click(getByText("Client pool A3"));
    fireEvent.click(getAllByText("Save").find((btn) => btn.closest("button")) as HTMLElement);
    await waitFor(() => expect(getByTestId("pool-row-2")).toBeInTheDocument());

    // "Add another pool" button should not be visible
    expect(queryByTestId("add-another-pool-button")).not.toBeInTheDocument();
  });

  test("calls onAssignPools with correct pool IDs when assign button clicked", async () => {
    const { getByText, getByTestId, getAllByText } = render(
      <PoolSelectionPage deviceIdentifiers={deviceIdentifiers} onAssignPools={onAssignPools} onDismiss={onCancel} />,
    );

    // Add first pool
    fireEvent.click(getByTestId("add-pool-button"));
    await waitFor(() => expect(getByText("Select pool")).toBeInTheDocument());
    fireEvent.click(getByText("Client pool A1"));
    fireEvent.click(getAllByText("Save").find((btn) => btn.closest("button")) as HTMLElement);
    await waitFor(() => expect(getByTestId("pool-row-0")).toBeInTheDocument());

    // Add second pool
    fireEvent.click(getByTestId("add-another-pool-button"));
    await waitFor(() => expect(getByText("Select pool")).toBeInTheDocument());
    fireEvent.click(getByText("Client pool A2"));
    fireEvent.click(getAllByText("Save").find((btn) => btn.closest("button")) as HTMLElement);
    await waitFor(() => expect(getByTestId("pool-row-1")).toBeInTheDocument());

    // Click assign button
    const assignButton = getByText(`Assign to ${numberOfMiners} miners`).closest("button") as HTMLElement;
    fireEvent.click(assignButton);

    await waitFor(() => {
      expect(onAssignPools).toHaveBeenCalledWith({
        defaultPool: { type: "poolId", poolId: "1" },
        backup1Pool: { type: "poolId", poolId: "2" },
        backup2Pool: undefined,
      });
    });
  });

  test("shows priority numbers in pool list", async () => {
    const { getByText, getByTestId, getAllByText } = render(
      <PoolSelectionPage deviceIdentifiers={deviceIdentifiers} onAssignPools={onAssignPools} onDismiss={onCancel} />,
    );

    // Add first pool
    fireEvent.click(getByTestId("add-pool-button"));
    await waitFor(() => expect(getByText("Select pool")).toBeInTheDocument());
    fireEvent.click(getByText("Client pool A1"));
    fireEvent.click(getAllByText("Save").find((btn) => btn.closest("button")) as HTMLElement);
    await waitFor(() => expect(getByTestId("pool-row-0")).toBeInTheDocument());

    // Add second pool
    fireEvent.click(getByTestId("add-another-pool-button"));
    await waitFor(() => expect(getByText("Select pool")).toBeInTheDocument());
    fireEvent.click(getByText("Client pool A2"));
    fireEvent.click(getAllByText("Save").find((btn) => btn.closest("button")) as HTMLElement);
    await waitFor(() => expect(getByTestId("pool-row-1")).toBeInTheDocument());

    // Check priority numbers are displayed
    const poolRow0 = getByTestId("pool-row-0");
    const poolRow1 = getByTestId("pool-row-1");

    expect(poolRow0).toHaveTextContent("1");
    expect(poolRow1).toHaveTextContent("2");
  });

  test("shows Add new pool button in selection modal", async () => {
    const { getByText, getByTestId } = render(
      <PoolSelectionPage deviceIdentifiers={deviceIdentifiers} onAssignPools={onAssignPools} onDismiss={onCancel} />,
    );

    fireEvent.click(getByTestId("add-pool-button"));

    await waitFor(() => {
      expect(getByText("Select pool")).toBeInTheDocument();
    });

    expect(getByTestId("add-new-pool-button")).toBeInTheDocument();
    expect(getByText("Add new pool")).toBeInTheDocument();
  });

  test("shows success callout when test connection succeeds", async () => {
    const { getByText, getByTestId, getAllByText } = render(
      <PoolSelectionPage deviceIdentifiers={deviceIdentifiers} onAssignPools={onAssignPools} onDismiss={onCancel} />,
    );

    // Add a pool first
    fireEvent.click(getByTestId("add-pool-button"));
    await waitFor(() => expect(getByText("Select pool")).toBeInTheDocument());
    fireEvent.click(getByText("Client pool A1"));
    fireEvent.click(getAllByText("Save").find((btn) => btn.closest("button")) as HTMLElement);
    await waitFor(() => expect(getByTestId("pool-row-0")).toBeInTheDocument());

    // Click test connection via the actions menu
    const actionsButton = getByTestId("pool-1-actions-menu-button");
    fireEvent.click(actionsButton);

    await waitFor(() => {
      expect(getByTestId("pool-1-test-connection-action")).toBeInTheDocument();
    });

    fireEvent.click(getByTestId("pool-1-test-connection-action"));

    // Success callout should appear and be visible (max-h-96)
    await waitFor(() => {
      const callout = getByTestId("pool-selection-page-connection-success-callout");
      expect(callout).toHaveClass("max-h-96");
      expect(callout).not.toHaveClass("max-h-0");
      expect(getByText("Pool connection successful")).toBeInTheDocument();
    });
  });

  test("dismisses success callout when dismiss button is clicked", async () => {
    const { getByText, getByTestId, getAllByText } = render(
      <PoolSelectionPage deviceIdentifiers={deviceIdentifiers} onAssignPools={onAssignPools} onDismiss={onCancel} />,
    );

    // Add a pool first
    fireEvent.click(getByTestId("add-pool-button"));
    await waitFor(() => expect(getByText("Select pool")).toBeInTheDocument());
    fireEvent.click(getByText("Client pool A1"));
    fireEvent.click(getAllByText("Save").find((btn) => btn.closest("button")) as HTMLElement);
    await waitFor(() => expect(getByTestId("pool-row-0")).toBeInTheDocument());

    // Click test connection via the actions menu
    const actionsButton = getByTestId("pool-1-actions-menu-button");
    fireEvent.click(actionsButton);

    await waitFor(() => {
      expect(getByTestId("pool-1-test-connection-action")).toBeInTheDocument();
    });

    fireEvent.click(getByTestId("pool-1-test-connection-action"));

    // Success callout should appear with max-h-96 (visible state)
    await waitFor(() => {
      const callout = getByTestId("pool-selection-page-connection-success-callout");
      expect(callout).toHaveClass("max-h-96");
    });

    // Find and click the dismiss button within the callout
    const callout = getByTestId("pool-selection-page-connection-success-callout");
    const dismissButton = callout.querySelector("button");
    if (dismissButton) {
      fireEvent.click(dismissButton);
    }

    // Callout should be hidden (max-h-0 class)
    await waitFor(() => {
      const calloutAfter = getByTestId("pool-selection-page-connection-success-callout");
      expect(calloutAfter).toHaveClass("max-h-0");
    });
  });

  test("shows error callout when test connection fails", async () => {
    // Override mock to simulate failure
    mockValidatePool.mockImplementationOnce(({ onError, onFinally }) => {
      onError?.();
      onFinally?.();
    });

    const { getByText, getByTestId, getAllByText } = render(
      <PoolSelectionPage deviceIdentifiers={deviceIdentifiers} onAssignPools={onAssignPools} onDismiss={onCancel} />,
    );

    // Add a pool first
    fireEvent.click(getByTestId("add-pool-button"));
    await waitFor(() => expect(getByText("Select pool")).toBeInTheDocument());
    fireEvent.click(getByText("Client pool A1"));
    fireEvent.click(getAllByText("Save").find((btn) => btn.closest("button")) as HTMLElement);
    await waitFor(() => expect(getByTestId("pool-row-0")).toBeInTheDocument());

    // Click test connection via the actions menu
    const actionsButton = getByTestId("pool-1-actions-menu-button");
    fireEvent.click(actionsButton);

    await waitFor(() => {
      expect(getByTestId("pool-1-test-connection-action")).toBeInTheDocument();
    });

    fireEvent.click(getByTestId("pool-1-test-connection-action"));

    // Error callout should appear and be visible (max-h-96)
    await waitFor(() => {
      const callout = getByTestId("pool-selection-page-connection-error-callout");
      expect(callout).toHaveClass("max-h-96");
      expect(callout).not.toHaveClass("max-h-0");
      expect(
        getByText("We couldn't connect with your pool. Review your pool details and try again."),
      ).toBeInTheDocument();
    });
  });

  test("dismisses callout when opening pool selection modal", async () => {
    const { getByText, getByTestId, getAllByText } = render(
      <PoolSelectionPage deviceIdentifiers={deviceIdentifiers} onAssignPools={onAssignPools} onDismiss={onCancel} />,
    );

    // Add a pool first
    fireEvent.click(getByTestId("add-pool-button"));
    await waitFor(() => expect(getByText("Select pool")).toBeInTheDocument());
    fireEvent.click(getByText("Client pool A1"));
    fireEvent.click(getAllByText("Save").find((btn) => btn.closest("button")) as HTMLElement);
    await waitFor(() => expect(getByTestId("pool-row-0")).toBeInTheDocument());

    // Click test connection via the actions menu
    const actionsButton = getByTestId("pool-1-actions-menu-button");
    fireEvent.click(actionsButton);

    await waitFor(() => {
      expect(getByTestId("pool-1-test-connection-action")).toBeInTheDocument();
    });

    fireEvent.click(getByTestId("pool-1-test-connection-action"));

    // Success callout should appear with max-h-96 (visible state)
    await waitFor(() => {
      const callout = getByTestId("pool-selection-page-connection-success-callout");
      expect(callout).toHaveClass("max-h-96");
    });

    // Open pool selection modal (Add another pool)
    fireEvent.click(getByTestId("add-another-pool-button"));

    // Callout should be hidden (max-h-0 class) when modal opens
    await waitFor(() => {
      const callout = getByTestId("pool-selection-page-connection-success-callout");
      expect(callout).toHaveClass("max-h-0");
    });
  });
});
