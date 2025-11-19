import { fireEvent, render } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import PoolsList from ".";

describe("Pools list", () => {
  const availablePools = [
    { poolUrl: "stratum+tcp://mine.ocean.xyz:3323", username: "user1" },
    { poolUrl: "stratum+tcp://mine.ocean.xyz:3324", username: "user2" },
  ];

  const onSelect = vi.fn();

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
        availablePools={availablePools}
        onSelect={onSelect}
        createNewLabel={addDefaultPoolLabel}
      />,
    );

    expect(getByText(defaultPoolTitle)).toBeInTheDocument();
    if (defaultPoolSubtitle) {
      expect(getByText(defaultPoolSubtitle)).toBeInTheDocument();
    }
    expect(getByText(addDefaultPoolLabel)).toBeInTheDocument();
    expect(
      getByRole("button", { name: addDefaultPoolLabel }),
    ).toBeInTheDocument();
  });

  test("renders pool card with backup pool and number badge", () => {
    const { getByText, getByRole } = render(
      <PoolsList
        title={backupPoolTitle}
        subtitle={backupPoolSubtitle}
        availablePools={availablePools}
        onSelect={onSelect}
        createNewLabel={addBackupPoolLabel}
        poolNumber={1}
      />,
    );

    expect(getByText(backupPoolTitle)).toBeInTheDocument();
    expect(getByText(backupPoolSubtitle)).toBeInTheDocument();
    expect(getByText(addBackupPoolLabel)).toBeInTheDocument();
    expect(
      getByRole("button", { name: addBackupPoolLabel }),
    ).toBeInTheDocument();
    expect(getByText("1")).toBeInTheDocument();
  });

  test("calls onSelect when Add pool button is clicked", () => {
    const { getByRole } = render(
      <PoolsList
        title={defaultPoolTitle}
        subtitle={defaultPoolSubtitle}
        availablePools={availablePools}
        onSelect={onSelect}
        createNewLabel={addDefaultPoolLabel}
      />,
    );

    fireEvent.click(getByRole("button", { name: addDefaultPoolLabel }));
    expect(onSelect).toHaveBeenCalledWith(availablePools[0].poolUrl);
  });
});
