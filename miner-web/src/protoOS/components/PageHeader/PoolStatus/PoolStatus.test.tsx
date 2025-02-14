import { fireEvent, render, within } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";

import PoolStatus from "./PoolStatus";

vi.mock("react-router-dom", () => ({
  ...vi.importActual("react-router-dom"),
  useNavigate: () => ({
    Navigation: vi.fn(),
  }),
}));

describe("Pool Status", () => {
  const onClickViewPools = vi.fn();
  const defaultPoolUrl = "test.com";
  const backupPoolUrl = "backup.com";
  const buttonLabel = "Mining pool";
  const iconTestId = "concentric-circles";
  const successIconClass = "text-intent-success-fill";
  const criticalIconClass = "text-intent-critical-fill";
  const aliveDefaultPool = {
    url: defaultPoolUrl,
    status: "Active" as const,
    priority: 0,
  };
  const deadDefaultPool = {
    url: defaultPoolUrl,
    status: "Dead" as const,
    priority: 0,
  };
  const aliveBackupPool = {
    url: backupPoolUrl,
    status: "Active" as const,
    priority: 1,
  };
  const deadBackupPool = {
    url: backupPoolUrl,
    status: "Dead" as const,
    priority: 1,
  };

  test("renders loading state of pool status widget", () => {
    const { getByTestId } = render(
      <PoolStatus loading onClickViewPools={onClickViewPools} />
    );

    expect(getByTestId("mining-pool-spinner")).toBeInTheDocument();
  });

  test("does not render popover on click if pool status widget is loading", () => {
    const { getByTestId, queryByTestId } = render(
      <PoolStatus loading onClickViewPools={onClickViewPools} />
    );
    const { getByText } = within(getByTestId("pool-status-widget"));
    const buttonElement = getByText(buttonLabel);
    fireEvent.click(buttonElement);
    expect(queryByTestId("pool-info-popover")).not.toBeInTheDocument();
  });

  test("renders connected state of pool status widget", () => {
    const { getByTestId } = render(
      <PoolStatus
        onClickViewPools={onClickViewPools}
        poolsInfo={[aliveDefaultPool]}
      />
    );

    expect(getByTestId(iconTestId)).toHaveClass(successIconClass);
  });

  test("renders pool status popover", () => {
    const { getByTestId } = render(
      <PoolStatus
        onClickViewPools={onClickViewPools}
        poolsInfo={[aliveDefaultPool]}
      />
    );
    let { getByText } = within(getByTestId("pool-status-widget"));
    const buttonElement = getByText(buttonLabel);
    fireEvent.click(buttonElement);

    getByText = within(getByTestId("pool-info-popover")).getByText;
    expect(getByTestId("pool-info-popover")).toBeInTheDocument();
    expect(getByText("Connected")).toBeInTheDocument();
    expect(getByText("Default Pool")).toBeInTheDocument();
    expect(getByText(defaultPoolUrl)).toBeInTheDocument();
  });

  test("connected pool status popover shows the alive pool", () => {
    const { getByTestId, queryByText } = render(
      <PoolStatus
        onClickViewPools={onClickViewPools}
        poolsInfo={[deadDefaultPool, aliveBackupPool]}
      />
    );
    let { getByText } = within(getByTestId("pool-status-widget"));
    const buttonElement = getByText(buttonLabel);
    fireEvent.click(buttonElement);

    getByText = within(getByTestId("pool-info-popover")).getByText;
    expect(queryByText("Default Pool")).not.toBeInTheDocument();
    expect(queryByText(defaultPoolUrl)).not.toBeInTheDocument();
    expect(getByText("Backup Pool #1")).toBeInTheDocument();
    expect(getByText(backupPoolUrl)).toBeInTheDocument();
  });

  test("renders no pools configured state of pool status widget", () => {
    const { getByTestId } = render(
      <PoolStatus onClickViewPools={onClickViewPools} />
    );

    expect(getByTestId(iconTestId)).toHaveClass(criticalIconClass);
  });

  test("renders no pools configured state in the popover", () => {
    const { getByTestId } = render(
      <PoolStatus onClickViewPools={onClickViewPools} />
    );
    const { getByText } = within(getByTestId("pool-status-widget"));
    const buttonElement = getByText(buttonLabel);
    fireEvent.click(buttonElement);
    expect(getByText("No mining pools")).toBeInTheDocument();
  });

  test("renders disconnected state of pool status widget", () => {
    const { getByTestId } = render(
      <PoolStatus
        onClickViewPools={onClickViewPools}
        poolsInfo={[deadDefaultPool]}
      />
    );

    expect(getByTestId(iconTestId)).toHaveClass(criticalIconClass);
  });

  test("renders disconnected state in the popover", () => {
    const { getByTestId } = render(
      <PoolStatus
        onClickViewPools={onClickViewPools}
        poolsInfo={[deadDefaultPool, deadBackupPool]}
      />
    );
    const { getByText } = within(getByTestId("pool-status-widget"));
    const buttonElement = getByText(buttonLabel);
    fireEvent.click(buttonElement);
    expect(getByText("Not connected")).toBeInTheDocument();
    expect(getByText(defaultPoolUrl)).toBeInTheDocument();
    expect(getByText(backupPoolUrl)).toBeInTheDocument();
  });

  test("closes popover on click view mining pools", () => {
    const { getByTestId, queryByTestId } = render(
      <PoolStatus
        onClickViewPools={onClickViewPools}
        shouldShowPopover
      />
    );
    const { getByText } = within(getByTestId("pool-info-popover"));
    const buttonElement = getByText("Add mining pools");
    fireEvent.click(buttonElement);
    expect(queryByTestId("pool-info-popover")).not.toBeInTheDocument();
  });
});
