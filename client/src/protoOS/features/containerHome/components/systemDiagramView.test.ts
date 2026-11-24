import { describe, expect, it } from "vitest";
import { constrainView, fitScale, fitView, MAX_ZOOM, resizeView, zoomView } from "./systemDiagramView";

describe("system diagram view", () => {
  const desktop = { width: 1200, height: 600 };
  const phone = { width: 361, height: 400 };

  it.each([desktop, phone])("fits the complete system with room around its edges at %o", (size) => {
    const view = fitView(size);
    expect(view.x).toBeGreaterThanOrEqual(16);
    expect(view.y).toBeGreaterThanOrEqual(16);
    expect(view.x + 1120 * view.scale).toBeLessThanOrEqual(size.width - 16);
    expect(view.y + 600 * view.scale).toBeLessThanOrEqual(size.height - 16);
  });

  it("allows shrinking below Fit while retaining a useful lower limit", () => {
    const fitted = fitView(phone);
    const smaller = zoomView(fitted, phone, fitted.scale / 1.35);
    expect(smaller.scale).toBeLessThan(fitted.scale);
    expect(zoomView(smaller, phone, 0).scale).toBe(fitted.scale / 2);
    expect(zoomView(smaller, phone, 100).scale).toBe(MAX_ZOOM);
  });

  it("holds the diagram point under the gesture center while zooming", () => {
    const view = { scale: 2, x: -500, y: -300 };
    const anchor = { x: 420, y: 260 };
    const zoomed = zoomView(view, desktop, 2.5, anchor);
    expect((anchor.x - zoomed.x) / zoomed.scale).toBeCloseTo((anchor.x - view.x) / view.scale);
    expect((anchor.y - zoomed.y) / zoomed.scale).toBeCloseTo((anchor.y - view.y) / view.scale);
  });

  it("keeps a dragged diagram in reach and centers content smaller than the canvas", () => {
    expect(constrainView({ scale: 2, x: 9000, y: -9000 }, desktop)).toEqual({ scale: 2, x: 16, y: -616 });
    const fitted = fitView(phone);
    expect(constrainView({ ...fitted, x: -9000, y: 9000 }, phone)).toEqual(fitted);
  });

  it("refits on resize only when the view was fitted, otherwise keeping the inspected location", () => {
    expect(resizeView(fitView(desktop), desktop, phone)).toEqual(fitView(phone));
    const inspected = { scale: 2, x: -600, y: -400 };
    const resized = resizeView(inspected, desktop, phone);
    expect(resized.scale).toBe(inspected.scale);
    expect((phone.width / 2 - resized.x) / resized.scale).toBe((desktop.width / 2 - inspected.x) / inspected.scale);
    expect((phone.height / 2 - resized.y) / resized.scale).toBe((desktop.height / 2 - inspected.y) / inspected.scale);
    expect(resized.scale).toBeGreaterThan(fitScale(phone));
  });
});
