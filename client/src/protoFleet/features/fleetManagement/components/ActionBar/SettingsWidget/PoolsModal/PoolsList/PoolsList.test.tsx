import { fireEvent, render } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import PoolsList from ".";
import { selectTypes } from "@/shared/constants";

describe("Pools list", () => {
  const availablePools = [
    { poolUrl: "stratum+tcp://mine.ocean.xyz:3323", username: "user1" },
    { poolUrl: "stratum+tcp://mine.ocean.xyz:3324", username: "user2" },
  ];

  const onSelect = vi.fn();

  const defaultPoolTitle = "Default pool";
  const defaultPoolSubtitle = "Select one default pool";
  const backupPoolTitle = "Backup pool";
  const backupPoolSubtitle = "Select up to two backup pools";
  const addDefaultPoolLabel = "Add default pool";
  const addBackupPoolLabel = "Add a backup pool";

  test("renders pool list with radio buttons", () => {
    const { getByText, getAllByRole } = render(
      <PoolsList
        title={defaultPoolTitle}
        subtitle={defaultPoolSubtitle}
        availablePools={availablePools}
        selectType={selectTypes.radio}
        selectedPools={[]}
        onSelect={onSelect}
        createNewLabel={addDefaultPoolLabel}
      />,
    );

    expect(getByText(defaultPoolTitle)).toBeInTheDocument();
    expect(getByText(defaultPoolSubtitle)).toBeInTheDocument();
    expect(getByText(addDefaultPoolLabel)).toBeInTheDocument();
    expect(getByText(availablePools[0].poolUrl)).toBeInTheDocument();
    expect(getByText(availablePools[0].username)).toBeInTheDocument();
    expect(getAllByRole("radio").length).toBe(2);
  });

  test("renders pool list with checkboxes", () => {
    const { getByText, getAllByRole } = render(
      <PoolsList
        title={backupPoolTitle}
        subtitle={backupPoolSubtitle}
        availablePools={availablePools}
        selectType={selectTypes.checkbox}
        selectedPools={[]}
        onSelect={onSelect}
        createNewLabel={addBackupPoolLabel}
      />,
    );

    expect(getByText(backupPoolTitle)).toBeInTheDocument();
    expect(getByText(backupPoolSubtitle)).toBeInTheDocument();
    expect(getByText(addBackupPoolLabel)).toBeInTheDocument();
    expect(getByText(availablePools[0].poolUrl)).toBeInTheDocument();
    expect(getByText(availablePools[0].username)).toBeInTheDocument();
    expect(getAllByRole("checkbox").length).toBe(2);
  });

  test("calls onSelect when a radio button is selected", () => {
    const { getAllByRole } = render(
      <PoolsList
        title={defaultPoolTitle}
        subtitle={defaultPoolSubtitle}
        availablePools={availablePools}
        selectType={selectTypes.radio}
        selectedPools={[]}
        onSelect={onSelect}
        createNewLabel={addDefaultPoolLabel}
      />,
    );

    fireEvent.click(getAllByRole("radio")[0]);
    expect(onSelect).toHaveBeenCalledWith(availablePools[0].poolUrl, true);
  });

  test("calls onSelect when a checkbox is selected", () => {
    const { getAllByRole } = render(
      <PoolsList
        title={backupPoolTitle}
        subtitle={backupPoolSubtitle}
        availablePools={availablePools}
        selectType={selectTypes.checkbox}
        selectedPools={[]}
        onSelect={onSelect}
        createNewLabel={addBackupPoolLabel}
      />,
    );

    fireEvent.click(getAllByRole("checkbox")[0]);
    expect(onSelect).toHaveBeenCalledWith(availablePools[0].poolUrl, true);
  });
});
