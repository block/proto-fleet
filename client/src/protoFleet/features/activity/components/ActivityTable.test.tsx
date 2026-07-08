import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import ActivityTable from "./ActivityTable";
import { ActivityEntrySchema } from "@/protoFleet/api/generated/activity/v1/activity_pb";

const fetchBatchResultsMock = vi.hoisted(() => vi.fn());

vi.mock("@/protoFleet/api/useCommandBatchDeviceResults", () => ({
  useCommandBatchDeviceResults: () => ({
    fetch: fetchBatchResultsMock,
    getResult: () => null,
  }),
}));

describe("ActivityTable", () => {
  const baseEntry = {
    eventCategory: "device_command",
    scopeType: "rack",
    scopeLabel: "Rack A",
    scopeCount: 2,
    actorType: "user",
    username: "alice",
    result: "success",
    createdAt: { seconds: 1_710_000_000n },
  };

  it("uses the displayed activity string as the detail modal title", () => {
    const entry = create(ActivityEntrySchema, {
      ...baseEntry,
      eventId: "activity-1",
      eventType: "reboot.completed",
      description: "Reboot completed miners",
    });

    render(<ActivityTable activities={[entry]} />);

    expect(screen.getByTestId("type")).toHaveTextContent("Rebooted miners");
    expect(screen.getByText("Rebooted miners")).toHaveClass("text-emphasis-300");
    expect(screen.getByText("1 activity")).toHaveClass("pb-4");
    expect(screen.getByText("1 activity")).not.toHaveClass("px-4");
    expect(screen.getByTestId("list-header")).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "Activity" })).not.toHaveClass("phone:hidden");
    expect(screen.getByTestId("list-row").tagName).toBe("TR");
    expect(screen.getByTestId("list-row")).not.toHaveClass("phone:grid-cols-1", "phone:px-0");
    expect(screen.getByTestId("list-header").closest("table")).toHaveClass("phone:table-fixed");
    expect(screen.getByTestId("type")).not.toHaveClass("tablet:sticky");
    expect(screen.getByTestId("scope")).toHaveTextContent("Rack A (2 miners)");
    expect(screen.getByTestId("time")).not.toHaveTextContent("Time");

    fireEvent.click(screen.getByTestId("list-row"));

    const modal = screen.getByTestId("modal");
    expect(within(modal).getByText("Rebooted miners")).toBeInTheDocument();
    expect(within(modal).queryByText("Actions")).not.toBeInTheDocument();
  });

  it("renders alert and destructive activity icons in critical red", () => {
    const failedLogin = create(ActivityEntrySchema, {
      ...baseEntry,
      eventId: "activity-1",
      eventCategory: "auth",
      eventType: "login_failed",
      description: "Login failed",
      result: "failure",
      errorMessage: "invalid credentials",
    });
    const deletedPool = create(ActivityEntrySchema, {
      ...baseEntry,
      eventId: "activity-2",
      eventType: "delete_pool",
      description: "Deleted pool",
    });

    render(<ActivityTable activities={[failedLogin, deletedPool]} />);

    expect(screen.getByText("Couldn't log in")).toBeInTheDocument();
    expect(screen.getByTestId("alert-icon").parentElement).toHaveClass("text-intent-critical-fill");
    expect(screen.queryByTestId("trash-icon")).not.toBeInTheDocument();
    expect(screen.getByTestId("minus-filled-icon").parentElement).toHaveClass("text-intent-critical-fill");

    fireEvent.click(screen.getAllByTestId("list-row")[0]);

    const modal = screen.getByTestId("modal");
    expect(within(modal).getByText("Couldn't complete")).toBeInTheDocument();
    expect(within(modal).getByText("Issue")).toBeInTheDocument();
    expect(within(modal).getByText("Credentials didn't match.")).toBeInTheDocument();
  });

  it("renders standalone dash table values in muted text", () => {
    const entry = create(ActivityEntrySchema, {
      eventId: "activity-1",
      eventType: "login",
      eventCategory: "auth",
      actorType: "user",
      result: "success",
      createdAt: { seconds: 1_710_000_000n },
    });

    render(<ActivityTable activities={[entry]} />);

    expect(screen.getByTestId("scope")).toHaveTextContent("—");
    expect(within(screen.getByTestId("scope")).getByText("—")).toHaveClass("text-text-primary-50");
    expect(screen.getByTestId("user")).toHaveTextContent("—");
    expect(within(screen.getByTestId("user")).getByText("—")).toHaveClass("text-text-primary-50");
  });
});
