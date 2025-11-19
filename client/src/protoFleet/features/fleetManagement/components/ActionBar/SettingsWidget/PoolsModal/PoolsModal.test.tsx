import { fireEvent, render, waitFor } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import PoolsModal from "./PoolsModal";

describe("Pools modal", () => {
  const numberOfMiners = 5;
  const availablePools = [
    { poolUrl: "stratum+tcp://mine.ocean.xyz:3323", username: "user1" },
    { poolUrl: "stratum+tcp://mine.ocean.xyz:3324", username: "user2" },
  ];

  const onDismiss = vi.fn();

  const defaultPoolTitle = "Default pool";
  const backupPool1Title = "Backup pool #1";
  const backupPool2Title = "Backup pool #2";

  test("renders modal with default and backup pools section", () => {
    const { getByText } = render(
      <PoolsModal
        numberOfMiners={numberOfMiners}
        availablePools={[]}
        onDismiss={onDismiss}
      />,
    );

    expect(getByText("Assign pools")).toBeInTheDocument();
    expect(getByText(defaultPoolTitle)).toBeInTheDocument();
    expect(getByText(backupPool1Title)).toBeInTheDocument();
    expect(getByText(backupPool2Title)).toBeInTheDocument();
  });

  test("renders correct number of miners in button text", () => {
    const { getByText } = render(
      <PoolsModal
        numberOfMiners={numberOfMiners}
        availablePools={[]}
        onDismiss={onDismiss}
      />,
    );

    expect(getByText(`Assign to ${numberOfMiners} miners`)).toBeInTheDocument();
  });

  test("calls onDismiss with poolsChanged=false when button clicked without changes", async () => {
    const { getByText } = render(
      <PoolsModal
        numberOfMiners={numberOfMiners}
        availablePools={[]}
        onDismiss={onDismiss}
      />,
    );

    fireEvent.click(getByText(`Assign to ${numberOfMiners} miners`));
    await waitFor(() => {
      expect(onDismiss).toHaveBeenCalledWith(false);
    });
  });

  test("calls onDismiss with poolsChanged=false when close button clicked without changes", async () => {
    const { getByTestId } = render(
      <PoolsModal
        numberOfMiners={numberOfMiners}
        availablePools={[]}
        onDismiss={onDismiss}
      />,
    );

    const closeModalButton = getByTestId("header-icon-button");
    fireEvent.click(closeModalButton);
    await waitFor(() => {
      expect(onDismiss).toHaveBeenCalledWith(false);
    });
  });

  test("calls onDismiss with poolsChanged=true when user selects default pool", async () => {
    const { getByText, getAllByText, getByTestId } = render(
      <PoolsModal
        numberOfMiners={numberOfMiners}
        availablePools={availablePools}
        onDismiss={onDismiss}
      />,
    );

    expect(getByText(`Assign to ${numberOfMiners} miners`)).toBeInTheDocument();

    // Click the "Add pool" button for the default pool (first one)
    const addPoolButtons = getAllByText("Add pool");
    fireEvent.click(addPoolButtons[0]);

    // Close modal
    const closeModalButton = getByTestId("header-icon-button");
    fireEvent.click(closeModalButton);

    await waitFor(() => {
      expect(onDismiss).toHaveBeenCalledWith(true);
    });
  });

  test("calls onDismiss with poolsChanged=true when user selects backup pool", async () => {
    const { getAllByText, getByTestId } = render(
      <PoolsModal
        numberOfMiners={numberOfMiners}
        availablePools={availablePools}
        onDismiss={onDismiss}
      />,
    );

    // Click the "Add pool" button for the first backup pool (second button)
    const addPoolButtons = getAllByText("Add pool");
    fireEvent.click(addPoolButtons[1]);

    // Close modal
    const closeModalButton = getByTestId("header-icon-button");
    fireEvent.click(closeModalButton);

    await waitFor(() => {
      expect(onDismiss).toHaveBeenCalledWith(true);
    });
  });

  test("prevents selecting the same pool for both backup slots", async () => {
    const { getAllByText, getByTestId } = render(
      <PoolsModal
        numberOfMiners={numberOfMiners}
        availablePools={availablePools}
        onDismiss={onDismiss}
      />,
    );

    const addPoolButtons = getAllByText("Add pool");

    // Select a pool for backup slot 1 (index 1 = second "Add pool" button)
    fireEvent.click(addPoolButtons[1]);

    // Try to select the same pool for backup slot 2 (index 2 = third "Add pool" button)
    fireEvent.click(addPoolButtons[2]);

    // Close modal - should still report poolsChanged=true (first selection was valid)
    const closeModalButton = getByTestId("header-icon-button");
    fireEvent.click(closeModalButton);

    await waitFor(() => {
      expect(onDismiss).toHaveBeenCalledWith(true);
    });
  });
});
