import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import TankHealthModule from "./TankHealthModule";
import type { TankModuleState } from "@/protoFleet/features/containers/components/TankModuleGrid";

// 16-module tank read as 8 × 2, with modules 13 & 16 needing attention.
const COLS = 8;
const ROWS = 2;
const modules: TankModuleState[] = Array.from({ length: COLS * ROWS }, (_, i) =>
  i === 12 || i === 15 ? "attention" : "healthy",
);

describe("TankHealthModule", () => {
  it("keeps collected physical slots anchored from the bottom left, including missing modules", () => {
    render(
      <TankHealthModule
        cols={2}
        rows={1}
        modules={[]}
        on={undefined}
        label="Tank 1"
        slots={[1, 9, 16].map((position) => ({
          id: String(position),
          position,
          label: `Module ${position}`,
          state: "healthy",
          row: 1,
          column: 1,
        }))}
      />,
    );
    const modules = screen.getAllByTestId("tank-module");
    expect(modules).toHaveLength(3);
    expect(modules[0].parentElement).toHaveStyle({ gridColumn: "1", gridRow: "2" });
    expect(modules[1].parentElement).toHaveStyle({ gridColumn: "1", gridRow: "1" });
    expect(modules[2].parentElement).toHaveStyle({ gridColumn: "8", gridRow: "1" });
  });

  it("preserves collected unknown, sleeping and isolated states without inventing empty modules", () => {
    const onSelectModule = vi.fn();
    render(
      <TankHealthModule
        cols={8}
        rows={1}
        modules={[]}
        on={undefined}
        label="Tank 2"
        slots={["unknown", "sleeping", "isolated"].map((state, i) => ({
          id: String(i),
          label: `Module ${i + 1}`,
          state: state as TankModuleState,
          row: 1,
          column: i + 1,
        }))}
        onSelectModule={onSelectModule}
      />,
    );
    expect(screen.getAllByTestId("tank-module").map((bar) => bar.dataset.moduleState)).toEqual([
      "unknown",
      "sleeping",
      "isolated",
    ]);
    expect(screen.queryByText("Unknown")).not.toBeInTheDocument();
    expect(screen.getByText("1 module unknown")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /Module 1.*unknown/ }).querySelector('[data-tank-status="unknown"]'),
    ).toBeInTheDocument();
    expect(screen.getByText("Sleeping")).toBeInTheDocument();
    expect(screen.queryByText("Isolated")).not.toBeInTheDocument();
    const isolated = screen.getByRole("button", { name: /Module 3.*Needs attention/ });
    expect(isolated.querySelector('[data-tank-status="attention"]')).toBeInTheDocument();
    expect(screen.getByText("Needs Attention").parentElement).toHaveTextContent("1 module");

    fireEvent.click(screen.getByRole("button", { name: /Module 1.*View module/ }));
    expect(onSelectModule).toHaveBeenCalledWith("0");
  });

  it("renders one numbered 3-segment bar per module, labelled 01..N", () => {
    render(<TankHealthModule cols={COLS} rows={ROWS} modules={modules} on label="Tank 2" />);

    const bars = screen.getAllByTestId("tank-module");
    expect(bars).toHaveLength(COLS * ROWS);

    // Row-major numbering 01..16 is present.
    expect(screen.getByText("01")).toBeInTheDocument();
    expect(screen.getByText("16")).toBeInTheDocument();

    // The attention modules carry the attention state (bright centre third).
    const attention = bars.filter((b) => b.getAttribute("data-module-state") === "attention");
    expect(attention).toHaveLength(2);
  });

  it("shows the unknown annotation only while modules have unknown states", () => {
    const props = { cols: 2, rows: 1, on: true, label: "Tank 1" };
    const { rerender } = render(<TankHealthModule {...props} modules={["unknown", "unknown"]} />);
    expect(screen.getByText("2 modules unknown")).toBeInTheDocument();
    expect(screen.queryByText("Unknown")).not.toBeInTheDocument();

    rerender(<TankHealthModule {...props} modules={["healthy", "sleeping"]} />);
    expect(screen.queryByText(/modules? unknown/)).not.toBeInTheDocument();
    expect(screen.queryByText("Unknown")).not.toBeInTheDocument();
  });

  it("summarises the tank's own state model (healthy / needs attention / offline) as module counts", () => {
    render(<TankHealthModule cols={COLS} rows={ROWS} modules={modules} on label="Tank 2" />);

    const panel = screen.getByTestId("tank-health-module");
    // 14 healthy, 2 needs attention, 0 offline when powered on.
    expect(within(panel).getByText("14 modules")).toBeInTheDocument();
    expect(within(panel).getByText("2 modules")).toBeInTheDocument();
    // Reads "modules", never the Fleet "miners" noun.
    expect(within(panel).queryByText(/miner/i)).not.toBeInTheDocument();
  });

  it("renders every module offline and disables module actions when the tank is powered off", () => {
    const onModuleAction = vi.fn();
    render(
      <TankHealthModule
        cols={COLS}
        rows={ROWS}
        modules={modules}
        on={false}
        label="Tank 6"
        onModuleAction={onModuleAction}
      />,
    );

    const panel = screen.getByTestId("tank-health-module");
    // All 16 offline, 0 healthy, 0 needs attention.
    expect(within(panel).getByText("16 modules")).toBeInTheDocument();
    const zeros = within(panel).getAllByText("0 modules");
    expect(zeros).toHaveLength(2);
    expect(screen.getAllByTestId("tank-module").every((bar) => bar.dataset.moduleState === "offline")).toBe(true);
    expect(screen.queryByLabelText("Tank 6 module 01 actions")).not.toBeInTheDocument();
    expect(onModuleAction).not.toHaveBeenCalled();
  });

  it("makes each bar an action-menu trigger when onModuleAction is supplied", () => {
    const onModuleAction = vi.fn();
    render(
      <TankHealthModule cols={COLS} rows={ROWS} modules={modules} on label="Tank 2" onModuleAction={onModuleAction} />,
    );

    // Open the first module's menu and pick Reboot.
    fireEvent.click(screen.getByLabelText("Tank 2 module 01 actions"));
    fireEvent.click(screen.getByTestId("module-action-reboot"));

    expect(onModuleAction).toHaveBeenCalledWith(0, "reboot");
  });

  it("renders plain status bars (no action trigger) when onModuleAction is omitted", () => {
    render(<TankHealthModule cols={COLS} rows={ROWS} modules={modules} on label="Tank 2" />);
    expect(screen.queryByLabelText("Tank 2 module 01 actions")).not.toBeInTheDocument();
  });
});
