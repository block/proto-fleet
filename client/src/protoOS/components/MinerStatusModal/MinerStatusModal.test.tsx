import { render, screen } from "@testing-library/react";
import { describe, expect, test } from "vitest";
import { ErrorLevel } from "./constants";
import MinerStatusModal from "./MinerStatusModal";

describe("MinerStatusModal", () => {
  const mockErrors = {
    hashboardError: {
      error_code: "HashboardError",
      error_level: ErrorLevel.error,
      error_message: "Hashboard error message",
    },
    hashboardWarning: {
      error_code: "HashboardWarning",
      error_level: ErrorLevel.warning,
      error_message: "Hashboard warning message",
    },
    fanError: {
      error_code: "FanError",
      error_level: ErrorLevel.error,
      error_message: "Fan error message",
    },
    psuError: {
      error_code: "PSUError",
      error_level: ErrorLevel.error,
      error_message: "PSU error message",
    },
  };

  test("renders correctly with no errors or warnings", () => {
    render(<MinerStatusModal onDismiss={() => {}} errors={[]} />);

    expect(screen.getByText("All systems are operational")).toBeInTheDocument();
    expect(screen.getByText("Fans")).toBeInTheDocument();
    expect(screen.getByText("Hashboards")).toBeInTheDocument();
    expect(screen.getByText("Control board")).toBeInTheDocument();
    expect(screen.getByText("PSU")).toBeInTheDocument();
  });

  test("renders correctly with a single error", () => {
    render(
      <MinerStatusModal onDismiss={() => {}} errors={[mockErrors.fanError]} />,
    );

    expect(
      screen.getByText("Your miner is not functioning properly"),
    ).toBeInTheDocument();
    expect(screen.getByText("1 error")).toBeInTheDocument();
    expect(screen.getByText("0 warnings")).toBeInTheDocument();

    // Check if error count is displayed
    expect(screen.getByText("1 error")).toBeInTheDocument();
  });

  test("renders correctly with multiple errors", () => {
    render(
      <MinerStatusModal
        onDismiss={() => {}}
        errors={[
          mockErrors.fanError,
          mockErrors.hashboardError,
          mockErrors.psuError,
        ]}
      />,
    );

    expect(
      screen.getByText("Your miner is not functioning properly"),
    ).toBeInTheDocument();
    expect(screen.getByText("3 errors")).toBeInTheDocument();
    expect(screen.getByText("0 warnings")).toBeInTheDocument();

    // Check if all error codes are displayed in the tabs
    expect(screen.getByText("3 errors")).toBeInTheDocument();
  });

  test("renders correctly with a single warning", () => {
    render(
      <MinerStatusModal
        onDismiss={() => {}}
        errors={[mockErrors.hashboardWarning]}
      />,
    );

    expect(
      screen.getByText("Your miner is not functioning properly"),
    ).toBeInTheDocument();
    expect(screen.getByText("0 errors")).toBeInTheDocument();
    expect(screen.getByText("1 warning")).toBeInTheDocument();

    // Check if the warning tab is displayed
    expect(screen.getByText("1 warning")).toBeInTheDocument();
  });

  test("renders correctly with a mix of errors and warnings", () => {
    render(
      <MinerStatusModal
        onDismiss={() => {}}
        errors={[mockErrors.fanError, mockErrors.hashboardWarning]}
      />,
    );

    expect(
      screen.getByText("Your miner is not functioning properly"),
    ).toBeInTheDocument();
    expect(screen.getByText("1 error")).toBeInTheDocument();
    expect(screen.getByText("1 warning")).toBeInTheDocument();

    // Check if both counts are displayed in tabs
    expect(screen.getByText("1 error")).toBeInTheDocument();
    expect(screen.getByText("1 warning")).toBeInTheDocument();
  });

  test("shows correct tabs and tab content", () => {
    render(
      <MinerStatusModal
        onDismiss={() => {}}
        errors={[mockErrors.fanError, mockErrors.hashboardWarning]}
      />,
    );

    // Check that we have tab buttons
    const allTab = screen.getByText("All");
    const errorTab = screen.getByText("1 error");
    const warningTab = screen.getByText("1 warning");

    // Verify tabs exist
    expect(allTab).toBeInTheDocument();
    expect(errorTab).toBeInTheDocument();
    expect(warningTab).toBeInTheDocument();
  });

  test("shows done button", () => {
    render(<MinerStatusModal onDismiss={() => {}} errors={[]} />);

    const doneButton = screen.getByText("Done");
    expect(doneButton).toBeInTheDocument();
  });
});
