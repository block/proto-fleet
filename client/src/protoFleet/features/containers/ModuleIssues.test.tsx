import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { ModuleIssues } from "./ModuleIssues";
import { InventoryItemSchema } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";

const item = (complete = true) =>
  create(InventoryItemSchema, {
    name: "CT1 T1 M1",
    state: "error",
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
        {
          component: "psu",
          componentId: "psu",
          cause: "PSU_OVER_TEMPERATURE_WARNING",
          severity: "warning",
          observedAt: timestampFromDate(new Date()),
        },
      ],
    },
  }).moduleIssues!.items;
  const rowClick = vi.fn();
  render(
    <div onClick={rowClick}>
      <ModuleIssues item={value} />
    </div>,
  );
  fireEvent.click(screen.getByRole("button", { name: "CT1 T1 M1 issues: Multiple failures" }));
  expect(rowClick).not.toHaveBeenCalled();
  expect(screen.getByTestId("module-issues")).toHaveTextContent("PSU · Over temperature warning");
  expect(screen.getByTestId("module-issues")).toHaveTextContent("MAC address: aa:bb");
  expect(screen.getByTestId("module-issues")).toHaveTextContent("Fault · Observed");
  expect(screen.getByTestId("module-issues")).toHaveTextContent("Warning");
  expect(within(screen.getByTestId("module-issues")).getByTestId("alert-icon").parentElement).toHaveClass(
    "text-intent-critical-fill",
  );
  expect(within(screen.getByTestId("module-issues")).getByTestId("alert-icon").parentElement).not.toHaveClass(
    "bg-surface-5",
  );
  expect(
    within(screen.getByTestId("module-issues")).getByTestId("hashboard-icon").parentElement?.parentElement,
  ).toHaveClass("text-text-primary-70");
  expect(
    within(screen.getByTestId("module-issues")).getByTestId("lightning-alt-icon").parentElement?.parentElement,
  ).toHaveClass("text-text-primary-70");
  expect(screen.getByTestId("module-issues")).toHaveTextContent(
    "Issue report is partial. Additional issues may be missing.",
  );
  expect(screen.getByTestId("module-issues")).not.toHaveTextContent("Current health is not confirmed");
  fireEvent.click(screen.getByRole("button", { name: "Done" }));
  expect(rowClick).not.toHaveBeenCalled();
});

it("distinguishes mixed-state observations from faults", () => {
  const value = create(InventoryItemSchema, {
    name: "Module",
    moduleIssues: {
      observedAt: timestampFromDate(new Date()),
      complete: true,
      items: [
        {
          component: "module",
          cause: "MIXED_BOARD_STATES",
          severity: "observation",
          observedAt: timestampFromDate(new Date()),
        },
      ],
    },
  });
  render(<ModuleIssues item={value} />);
  fireEvent.click(screen.getByRole("button"));
  expect(screen.getByTestId("module-issues")).toHaveTextContent("Observation");
  expect(screen.getByTestId("module-issues")).not.toHaveTextContent("Fault");
  expect(screen.queryByTestId("alert-icon")).not.toBeInTheDocument();
  expect(screen.getByTestId("module-issues").querySelector(".text-text-primary-30")).toBeInTheDocument();
});

it.each(["missing", "partial", "stale", "healthy", "attention"])(
  "leaves the issues column blank for %s evidence without current causes",
  (kind) => {
    const value = item(kind === "healthy" || kind === "attention");
    if (kind === "missing") value.moduleIssues = undefined;
    if (kind === "stale") value.moduleIssues!.stale = true;
    if (kind === "healthy") value.state = "hashing";
    const { container } = render(<ModuleIssues item={value} />);
    expect(container).toBeEmptyDOMElement();
  },
);

it("expires cached issue evidence when refreshes stop", () => {
  vi.useFakeTimers();
  try {
    const value = create(InventoryItemSchema, {
      name: "Module",
      moduleIssues: {
        observedAt: timestampFromDate(new Date()),
        complete: true,
        items: [
          {
            component: "psu",
            cause: "PSU_OVER_TEMPERATURE",
            severity: "fault",
            observedAt: timestampFromDate(new Date()),
          },
        ],
      },
    });
    render(<ModuleIssues item={value} />);
    expect(screen.getByRole("button")).toHaveTextContent("PSU failure");
    act(() => vi.advanceTimersByTime(120_000));
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  } finally {
    vi.useRealTimers();
  }
});

it("shows fresh faults alongside stale board evidence and expires each cause independently", () => {
  vi.useFakeTimers();
  try {
    const now = Date.now();
    const value = create(InventoryItemSchema, {
      name: "Module",
      moduleIssues: {
        observedAt: timestampFromDate(new Date(now - 180_000)),
        complete: false,
        stale: true,
        items: [
          {
            component: "hashboard",
            componentId: "right",
            cause: "BOARD_OVER_CURRENT",
            severity: "fault",
            observedAt: timestampFromDate(new Date(now)),
          },
          {
            component: "psu",
            cause: "PSU_OVER_TEMPERATURE",
            severity: "fault",
            observedAt: timestampFromDate(new Date(now - 30_000)),
          },
        ],
      },
    });
    render(<ModuleIssues item={value} />);
    fireEvent.click(screen.getByRole("button", { name: "Module issues: Multiple failures" }));
    expect(screen.getByTestId("module-issues")).toHaveTextContent(
      "Issue report is partial. Additional issues may be missing.",
    );
    act(() => vi.advanceTimersByTime(90_000));
    expect(screen.getByRole("button", { name: "Module issues: Hashboard failure" })).toBeInTheDocument();
    expect(screen.getByTestId("module-issues")).not.toHaveTextContent("PSU · Over temperature");
    act(() => vi.advanceTimersByTime(30_000));
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    expect(screen.queryByTestId("module-issues")).not.toBeInTheDocument();
  } finally {
    vi.useRealTimers();
  }
});
