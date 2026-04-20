import { fireEvent, render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import GroupedToaster from "./GroupedToaster";
import { STATUSES } from "@/shared/features/toaster";

describe("Grouped toaster", () => {
  const header = "grouped-toaster-header";
  const headerProgress = "header-progress-circular";
  const loadingProgress = "loading-progress-circular";
  const progressingProgress = "progressing-progress-circular";
  const queuedProgress = "queued-progress-circular";

  it("renders without crashing when no toasts are provided", () => {
    const { queryByText } = render(<GroupedToaster toasts={[]} />);
    expect(queryByText("updates in progress")).not.toBeInTheDocument();
  });

  it("displays toasts correctly", () => {
    const toasts = [
      { id: 1, message: "Toast 1", status: STATUSES.loading },
      { id: 2, message: "Toast 2", status: STATUSES.loading, progress: 50 },
      { id: 3, message: "Toast 3", status: STATUSES.queued },
    ];

    const { getByTestId, getByText } = render(<GroupedToaster toasts={toasts} />);

    expect(getByText("3 updates in progress")).toBeInTheDocument();
    expect(getByTestId(headerProgress)).toBeInTheDocument();

    const headerElement = getByTestId(header);
    fireEvent.click(headerElement);

    expect(getByTestId(loadingProgress)).toBeInTheDocument();
    expect(getByTestId(progressingProgress)).toBeInTheDocument();
    expect(getByTestId(queuedProgress)).toBeInTheDocument();
  });

  it("renders loading toast correctly", () => {
    const toasts = [{ id: 1, message: "Loading action", status: STATUSES.loading }];

    const { getAllByText, getByTestId } = render(<GroupedToaster toasts={toasts} />);
    const headerElement = getByTestId(header);
    let progress = getByTestId(headerProgress);
    expect(progress).toBeInTheDocument();
    expect(progress).toHaveClass("animate-spin");
    fireEvent.click(headerElement);

    expect(getAllByText(toasts[0].message)).toHaveLength(1);
    progress = getByTestId(loadingProgress);
    expect(progress).toBeInTheDocument();
    expect(progress).toHaveClass("animate-spin");
  });

  it("renders progressing toast correctly", () => {
    const toasts = [
      {
        id: 1,
        message: "Progressing action",
        status: STATUSES.loading,
        progress: 50,
      },
    ];

    const { getAllByText, getByText, getByTestId } = render(<GroupedToaster toasts={toasts} />);
    const headerElement = getByTestId(header);
    expect(getByTestId(headerProgress)).toBeInTheDocument();
    fireEvent.click(headerElement);

    expect(getAllByText(toasts[0].message)).toHaveLength(1);
    expect(getByText("50% complete")).toBeInTheDocument();
    expect(getByTestId(progressingProgress)).toBeInTheDocument();
  });

  it("renders queued toast correctly", () => {
    const toasts = [
      {
        id: 1,
        message: "Queued action",
        status: STATUSES.queued,
      },
    ];

    const { getAllByText, getByText, getByTestId } = render(<GroupedToaster toasts={toasts} />);
    const headerElement = getByTestId(header);
    expect(getByTestId(headerProgress)).toBeInTheDocument();
    fireEvent.click(headerElement);

    expect(getAllByText(toasts[0].message)).toHaveLength(1);
    expect(getByText("Queued")).toBeInTheDocument();
    expect(getByTestId(queuedProgress)).toBeInTheDocument();
  });

  it("does not show duplicate message text when single toast is expanded", () => {
    const toasts = [{ id: 1, message: "Blinking LEDs", status: STATUSES.loading }];

    const { getAllByText, getByTestId, queryByTestId } = render(<GroupedToaster toasts={toasts} />);
    const headerElement = getByTestId(header);
    fireEvent.click(headerElement);

    expect(getAllByText("Blinking LEDs")).toHaveLength(1);
    expect(queryByTestId(header)).not.toBeInTheDocument();
  });

  it("calls custom onClose callback when toast is removed", async () => {
    vi.useFakeTimers();
    const onCloseMock = vi.fn();
    const toasts = [
      {
        id: 1,
        message: "Success toast",
        status: STATUSES.success,
        ttl: 500,
        onClose: onCloseMock,
      },
    ];

    const { getByTestId } = render(<GroupedToaster toasts={toasts} />);

    const headerElement = getByTestId(header);
    fireEvent.click(headerElement);

    expect(onCloseMock).not.toHaveBeenCalled();

    vi.advanceTimersByTime(600);
    await vi.runOnlyPendingTimersAsync();

    expect(onCloseMock).toHaveBeenCalledOnce();

    vi.useRealTimers();
  });

  it("calls custom onClose for completed toasts on auto-cleanup", async () => {
    vi.useFakeTimers();
    const onCloseMock = vi.fn();
    const toasts = [
      {
        id: 1,
        message: "Success toast",
        status: STATUSES.success,
        ttl: 1000,
        onClose: onCloseMock,
      },
    ];

    render(<GroupedToaster toasts={toasts} />);

    expect(onCloseMock).not.toHaveBeenCalled();

    vi.advanceTimersByTime(1100);

    await vi.runOnlyPendingTimersAsync();

    expect(onCloseMock).toHaveBeenCalledOnce();

    vi.useRealTimers();
  });

  it("does not auto-cleanup when toaster is expanded", async () => {
    vi.useFakeTimers();
    const onCloseMock = vi.fn();
    const toasts = [
      {
        id: 1,
        message: "Success toast",
        status: STATUSES.success,
        ttl: false as const, // Disable TTL to test expansion behavior
        onClose: onCloseMock,
      },
    ];

    const { getByTestId } = render(<GroupedToaster toasts={toasts} />);

    // Expand the toaster immediately
    const headerElement = getByTestId(header);
    fireEvent.click(headerElement);

    // When expanded, the GroupedToaster's cleanup timer should be cleared
    // Individual toasts may still have their own timers, but GroupedToaster won't bulk-remove them
    // Since we set ttl: false, the individual toast won't auto-close either
    vi.advanceTimersByTime(2000);
    await vi.runOnlyPendingTimersAsync();

    // Verify onClose was NOT called
    expect(onCloseMock).not.toHaveBeenCalled();

    vi.useRealTimers();
  });

  it("renders action buttons on toasts that have actions", () => {
    const onClickMock = vi.fn();
    const toasts = [
      {
        id: 1,
        message: "Reboot failed on 3 out of 5 miners",
        status: STATUSES.error,
        longRunning: true,
        actions: [{ label: "Retry", onClick: onClickMock }],
      },
    ];

    const { getByTestId, getByText } = render(<GroupedToaster toasts={toasts} />);

    const headerElement = getByTestId(header);
    fireEvent.click(headerElement);

    const retryButton = getByText("Retry");
    expect(retryButton).toBeInTheDocument();

    fireEvent.click(retryButton);
    expect(onClickMock).toHaveBeenCalledOnce();
  });

  it("does not render action buttons on toasts without actions", () => {
    const toasts = [
      {
        id: 1,
        message: "Reboot failed on 3 out of 5 miners",
        status: STATUSES.error,
        longRunning: true,
      },
    ];

    const { getByTestId, queryByTestId } = render(<GroupedToaster toasts={toasts} />);

    const headerElement = getByTestId(header);
    fireEvent.click(headerElement);

    expect(queryByTestId("toast-action-retry")).not.toBeInTheDocument();
  });
});
