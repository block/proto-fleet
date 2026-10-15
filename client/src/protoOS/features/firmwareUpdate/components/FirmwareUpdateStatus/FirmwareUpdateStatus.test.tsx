import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import FirmwareUpdateStatus from "./FirmwareUpdateStatus";

describe("FirmwareUpdateStatus", () => {
  it("hides the widget when firmware is current without an error", () => {
    const { container } = render(
      <FirmwareUpdateStatus updateStatus={{ status: "current", current_version: "1.0.0" }} />,
    );

    expect(container.firstChild).toHaveClass("hidden");
  });

  it("shows a failure reported with a current status", () => {
    const { container } = render(
      <FirmwareUpdateStatus
        updateStatus={{
          status: "current",
          current_version: "1.0.0",
          error: "Download failed: connection reset",
          message: "Last update attempt failed: Download failed: connection reset",
        }}
      />,
    );

    expect(container.firstChild).not.toHaveClass("hidden");
    const widget = screen.getByTestId("firmware-status-widget");
    expect(widget).toHaveTextContent("Update failed");

    fireEvent.click(widget);

    const modal = screen.getByTestId("firmware-status-modal");
    expect(within(modal).getByText("Update error")).toBeInTheDocument();
    expect(
      within(modal).getByText("Last update attempt failed: Download failed: connection reset"),
    ).toBeInTheDocument();
    expect(within(modal).queryByText("Firmware is up to date")).not.toBeInTheDocument();
  });

  it("keeps offering install when a failure is reported with an available update", () => {
    render(
      <FirmwareUpdateStatus
        updateStatus={{
          status: "available",
          current_version: "1.0.0",
          new_version: "1.1.0",
          error: "Download failed",
          message: "Last update attempt failed: Download failed",
        }}
      />,
    );

    const widget = screen.getByTestId("firmware-status-widget");
    expect(widget).toHaveTextContent("Update available");

    fireEvent.click(widget);

    const modal = screen.getByTestId("firmware-status-modal");
    expect(within(modal).getByText("Last update attempt failed: Download failed")).toBeInTheDocument();
    expect(within(modal).getByRole("button", { name: "Install" })).toBeInTheDocument();
  });
});
