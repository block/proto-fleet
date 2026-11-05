import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ContainerSystemViewport from "./ContainerSystemViewport";

describe("live system diagram viewport", () => {
  let resize: (width: number, height: number) => void;

  beforeEach(() => {
    vi.stubGlobal(
      "ResizeObserver",
      class {
        constructor(callback: (entries: { contentRect: { width: number; height: number } }[]) => void) {
          resize = (width, height) => callback([{ contentRect: { width, height } }]);
        }
        observe() {}
        disconnect() {}
      },
    );
  });
  afterEach(() => vi.unstubAllGlobals());

  it("keeps the inspected view and equipment clicks when live readings change", () => {
    const manage = vi.fn();
    const reading = (value: string) => (
      <ContainerSystemViewport>
        <button onClick={manage}>Pump {value}</button>
      </ContainerSystemViewport>
    );
    const { rerender } = render(reading("50.0 Hz"));
    act(() => resize(1200, 600));
    fireEvent.click(screen.getByRole("button", { name: "Zoom in" }));
    const zoom = screen.getByLabelText("Diagram zoom level").textContent;
    rerender(reading("50.1 Hz"));
    expect(screen.getByLabelText("Diagram zoom level")).toHaveTextContent(zoom!);
    fireEvent.click(screen.getByRole("button", { name: "Pump 50.1 Hz" }));
    expect(manage).toHaveBeenCalledTimes(1);
  });

  it("releases its wheel interception when the diagram is removed", () => {
    const { unmount } = render(<ContainerSystemViewport>System</ContainerSystemViewport>);
    act(() => resize(1200, 600));
    const canvas = screen.getByRole("group", { name: "Diagram canvas" });
    const wheel = () => new WheelEvent("wheel", { ctrlKey: true, deltaY: -30, cancelable: true, bubbles: true });
    const mounted = wheel();
    fireEvent(canvas, mounted);
    expect(mounted.defaultPrevented).toBe(true);
    unmount();
    const removed = wheel();
    fireEvent(canvas, removed);
    expect(removed.defaultPrevented).toBe(false);
  });
});
