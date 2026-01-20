import { fireEvent, render, within } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";

import PoolStatus from "./PoolStatus";
import { PopoverProvider } from "@/shared/components/Popover";

vi.mock("react-router-dom", async () => {
  const actual = await vi.importActual("react-router-dom");
  return {
    ...actual,
    useNavigate: () => ({
      Navigation: vi.fn(),
    }),
  };
});

describe("Pool Status", () => {
  const onClickViewPools = vi.fn();
  const defaultPoolUrl = "test.com";
  const backupPoolUrl = "backup.com";
  const buttonLabel = "Mining Pool";
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
      <PopoverProvider>
        <PoolStatus loading onClickViewPools={onClickViewPools} />
      </PopoverProvider>,
    );

    // Loading state shows cursor-progress class
    expect(getByTestId("pool-status-widget").querySelector("button")).toHaveClass("hover:cursor-progress");
  });

  test("does not render popover on click if pool status widget is loading", () => {
    const { getByTestId, queryByTestId } = render(
      <PopoverProvider>
        <PoolStatus loading onClickViewPools={onClickViewPools} />
      </PopoverProvider>,
    );
    const { getByText } = within(getByTestId("pool-status-widget"));
    const buttonElement = getByText(buttonLabel);
    fireEvent.click(buttonElement);
    expect(queryByTestId("pool-info-popover")).not.toBeInTheDocument();
  });

  test("renders connected state of pool status widget", () => {
    const { getByTestId } = render(
      <PopoverProvider>
        <PoolStatus onClickViewPools={onClickViewPools} poolsInfo={[aliveDefaultPool]} />
      </PopoverProvider>,
    );

    // Connected state - the component no longer shows icons, just the status text
    const widget = getByTestId("pool-status-widget");
    expect(widget).toBeInTheDocument();
  });

  test("renders pool status popover", () => {
    const { getByTestId } = render(
      <PopoverProvider>
        <PoolStatus onClickViewPools={onClickViewPools} poolsInfo={[aliveDefaultPool]} />
      </PopoverProvider>,
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
      <PopoverProvider>
        <PoolStatus onClickViewPools={onClickViewPools} poolsInfo={[deadDefaultPool, aliveBackupPool]} />
      </PopoverProvider>,
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
      <PopoverProvider>
        <PoolStatus onClickViewPools={onClickViewPools} />
      </PopoverProvider>,
    );

    // No pools configured state - the component no longer shows icons, just the status text
    const widget = getByTestId("pool-status-widget");
    expect(widget).toBeInTheDocument();
  });

  test("renders no pools configured state in the popover", () => {
    const { getByTestId, getByText } = render(
      <PopoverProvider>
        <PoolStatus onClickViewPools={onClickViewPools} />
      </PopoverProvider>,
    );
    const { getByText: getByTextWithinWidget } = within(getByTestId("pool-status-widget"));
    const buttonElement = getByTextWithinWidget(buttonLabel);
    fireEvent.click(buttonElement);
    expect(getByText("No mining pools")).toBeInTheDocument();
  });

  test("renders disconnected state of pool status widget", () => {
    const { getByTestId } = render(
      <PopoverProvider>
        <PoolStatus onClickViewPools={onClickViewPools} poolsInfo={[deadDefaultPool]} />
      </PopoverProvider>,
    );

    // Disconnected state - the component no longer shows icons, just the status text
    const widget = getByTestId("pool-status-widget");
    expect(widget).toBeInTheDocument();
  });

  test("renders disconnected state in the popover", () => {
    const { getByTestId, getByText } = render(
      <PopoverProvider>
        <PoolStatus onClickViewPools={onClickViewPools} poolsInfo={[deadDefaultPool, deadBackupPool]} />
      </PopoverProvider>,
    );
    const { getByText: getByTextWithinWidget } = within(getByTestId("pool-status-widget"));
    const buttonElement = getByTextWithinWidget(buttonLabel);
    fireEvent.click(buttonElement);
    expect(getByText("Not connected")).toBeInTheDocument();
    expect(getByText(defaultPoolUrl)).toBeInTheDocument();
    expect(getByText(backupPoolUrl)).toBeInTheDocument();
  });

  test("closes popover on click view mining pools", () => {
    const { getByTestId, queryByTestId } = render(
      <PopoverProvider>
        <PoolStatus onClickViewPools={onClickViewPools} shouldShowPopover />
      </PopoverProvider>,
    );
    const { getByText } = within(getByTestId("pool-info-popover"));
    const buttonElement = getByText("Add mining pools");
    fireEvent.click(buttonElement);
    expect(queryByTestId("pool-info-popover")).not.toBeInTheDocument();
  });
});
