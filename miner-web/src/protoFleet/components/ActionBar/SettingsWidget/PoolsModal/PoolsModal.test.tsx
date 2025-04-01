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
  const backupPoolTitle = "Backup pool";
  const addDefaultPoolLabel = "Add default pool";
  const addBackupPoolLabel = "Add a backup pool";

  test("renders modal with default and backup pools section", () => {
    const { getByText } = render(
      <PoolsModal
        numberOfMiners={numberOfMiners}
        availablePools={[]}
        onDismiss={onDismiss}
      />,
    );

    expect(getByText("Mining pools")).toBeInTheDocument();
    expect(getByText(defaultPoolTitle)).toBeInTheDocument();
    expect(getByText(addDefaultPoolLabel)).toBeInTheDocument();

    expect(getByText(backupPoolTitle)).toBeInTheDocument();
    expect(getByText(addBackupPoolLabel)).toBeInTheDocument();
  });

  test("renders correct number of miners", () => {
    const { getByText } = render(
      <PoolsModal
        numberOfMiners={numberOfMiners}
        availablePools={[]}
        onDismiss={onDismiss}
      />,
    );

    expect(
      getByText(`Update the mining pools for ${numberOfMiners} miners.`),
    ).toBeInTheDocument();
  });

  test("calls onDismiss when done button is clicked", async () => {
    const { getByText } = render(
      <PoolsModal
        numberOfMiners={numberOfMiners}
        availablePools={[]}
        onDismiss={onDismiss}
      />,
    );

    fireEvent.click(getByText("Done"));
    await waitFor(() => {
      expect(onDismiss).toHaveBeenCalled();
    });
  });

  test("calls onDismiss when close button is clicked", async () => {
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
      expect(onDismiss).toHaveBeenCalled();
    });
  });

  test("changes done button when user selects default pool", () => {
    const { getByText, queryByText, getAllByRole } = render(
      <PoolsModal
        numberOfMiners={numberOfMiners}
        availablePools={availablePools}
        onDismiss={onDismiss}
      />,
    );

    expect(getByText("Done")).toBeInTheDocument();

    fireEvent.click(getAllByRole("radio")[0]);
    expect(getByText("Update pools")).toBeInTheDocument();
    expect(queryByText("Done")).not.toBeInTheDocument();
  });

  test("changes done button when user selects backup pool", () => {
    const { getByText, queryByText, getAllByRole } = render(
      <PoolsModal
        numberOfMiners={numberOfMiners}
        availablePools={availablePools}
        onDismiss={onDismiss}
      />,
    );

    expect(getByText("Done")).toBeInTheDocument();

    fireEvent.click(getAllByRole("checkbox")[0]);
    expect(getByText("Update pools")).toBeInTheDocument();
    expect(queryByText("Done")).not.toBeInTheDocument();
  });
});
