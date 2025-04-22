import { fireEvent, render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
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

    const { getByTestId, getByText } = render(
      <GroupedToaster toasts={toasts} />,
    );

    expect(getByText("3 updates in progress")).toBeInTheDocument();
    expect(getByTestId(headerProgress)).toBeInTheDocument();

    const headerElement = getByTestId(header);
    fireEvent.click(headerElement);

    expect(getByTestId(loadingProgress)).toBeInTheDocument();
    expect(getByTestId(progressingProgress)).toBeInTheDocument();
    expect(getByTestId(queuedProgress)).toBeInTheDocument();
  });

  it("renders loading toast correctly", () => {
    const toasts = [
      { id: 1, message: "Loading action", status: STATUSES.loading },
    ];

    const { getByText, getByTestId } = render(
      <GroupedToaster toasts={toasts} />,
    );
    const headerElement = getByTestId(header);
    let progress = getByTestId(headerProgress);
    expect(progress).toBeInTheDocument();
    expect(progress).toHaveClass("animate-spin");
    fireEvent.click(headerElement);

    expect(getByText(toasts[0].message)).toBeInTheDocument();
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

    const { getByText, getByTestId } = render(
      <GroupedToaster toasts={toasts} />,
    );
    const headerElement = getByTestId(header);
    expect(getByTestId(headerProgress)).toBeInTheDocument();
    fireEvent.click(headerElement);

    expect(getByText(toasts[0].message)).toBeInTheDocument();
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

    const { getByText, getByTestId } = render(
      <GroupedToaster toasts={toasts} />,
    );
    const headerElement = getByTestId(header);
    expect(getByTestId(headerProgress)).toBeInTheDocument();
    fireEvent.click(headerElement);

    expect(getByText(toasts[0].message)).toBeInTheDocument();
    expect(getByText("Queued")).toBeInTheDocument();
    expect(getByTestId(queuedProgress)).toBeInTheDocument();
  });
});
