import { act, fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { ModuleIssues } from "./ModuleIssues";
import { InventoryItemSchema } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";

const item = (complete = true) =>
  create(InventoryItemSchema, {
    name: "CT1 T1 M1",
    state: "attention",
    moduleIssues: { observedAt: timestampFromDate(new Date()), complete, items: [] },
  });

it("shows cause and component, and opens all timestamped details without opening the row", () => {
  const value = item(false);
  value.moduleIssues!.items = create(InventoryItemSchema, {
    moduleIssues: {
      items: [
        {
          component: "hashboard",
          componentId: "aa:bb",
          cause: "HASHBOARD_ERROR_CAUSE_BOARD_OVER_CURRENT",
          severity: "fault",
          observedAt: timestampFromDate(new Date()),
        },
        { component: "psu", componentId: "psu", cause: "PSU_OVER_TEMPERATURE_WARNING", severity: "warning" },
      ],
    },
  }).moduleIssues!.items;
  const rowClick = vi.fn();
  render(
    <div onClick={rowClick}>
      <ModuleIssues item={value} />
    </div>,
  );
  fireEvent.click(screen.getByRole("button", { name: "CT1 T1 M1 issues: Hashboard · Over current (+1)" }));
  expect(rowClick).not.toHaveBeenCalled();
  expect(screen.getByTestId("module-issues")).toHaveTextContent("PSU · Over temperature warning");
  expect(screen.getByTestId("module-issues")).toHaveTextContent("MAC address: aa:bb");
  expect(screen.getByTestId("module-issues")).toHaveTextContent("Fault · Observed");
  expect(screen.getByTestId("module-issues")).toHaveTextContent("Warning");
  expect(screen.getByTestId("module-issues")).toHaveTextContent(
    "Issue report is partial. Additional issues may be missing.",
  );
  expect(screen.getByTestId("module-issues")).not.toHaveTextContent("Current health is not confirmed");
  fireEvent.click(screen.getByRole("button", { name: "Close" }));
  expect(rowClick).not.toHaveBeenCalled();
});

it("distinguishes mixed-state observations from faults", () => {
  const value = create(InventoryItemSchema, {
    name: "Module",
    moduleIssues: {
      observedAt: timestampFromDate(new Date()),
      complete: true,
      items: [{ component: "module", cause: "MIXED_BOARD_STATES", severity: "observation" }],
    },
  });
  render(<ModuleIssues item={value} />);
  fireEvent.click(screen.getByRole("button"));
  expect(screen.getByTestId("module-issues")).toHaveTextContent("Observation");
  expect(screen.getByTestId("module-issues")).not.toHaveTextContent("Fault");
});

it.each(["missing", "partial", "stale"])("does not imply health for %s evidence", (kind) => {
  const value = item(false);
  if (kind === "missing") value.moduleIssues = undefined;
  if (kind === "stale") value.moduleIssues!.stale = true;
  render(<ModuleIssues item={value} />);
  fireEvent.click(screen.getByRole("button"));
  expect(screen.getByTestId("module-issues")).toHaveTextContent("Current health is not confirmed");
  expect(screen.getByTestId("module-issues")).not.toHaveTextContent("No reported issues");
});

it("does not invent a cause from attention state", () => {
  render(<ModuleIssues item={item()} />);
  expect(screen.getByRole("button")).toHaveTextContent("No reported cause");
});

it("expires cached issue evidence when refreshes stop", () => {
  vi.useFakeTimers();
  try {
    const value = create(InventoryItemSchema, {
      name: "Module",
      moduleIssues: {
        observedAt: timestampFromDate(new Date()),
        complete: true,
        items: [{ component: "psu", cause: "PSU_OVER_TEMPERATURE", severity: "fault" }],
      },
    });
    render(<ModuleIssues item={value} />);
    expect(screen.getByRole("button")).toHaveTextContent("PSU · Over temperature");
    act(() => vi.advanceTimersByTime(120_000));
    expect(screen.getByRole("button")).toHaveTextContent("Issues stale");
  } finally {
    vi.useRealTimers();
  }
});
