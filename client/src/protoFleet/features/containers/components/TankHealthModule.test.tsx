import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import TankHealthModule from "./TankHealthModule";
import type { TankModuleState } from "./TankModuleGrid";

// 16-module tank read as 8 × 2, with modules 13 & 16 needing attention.
const COLS = 8;
const ROWS = 2;
const modules: TankModuleState[] = Array.from({ length: COLS * ROWS }, (_, i) =>
  i === 12 || i === 15 ? "attention" : "healthy",
);

describe("TankHealthModule", () => {
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
