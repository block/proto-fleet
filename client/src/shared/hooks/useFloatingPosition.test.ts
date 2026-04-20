import type { MutableRefObject } from "react";
import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { computeFloatingStyle, useFloatingPosition } from "./useFloatingPosition";
import type { Viewport } from "./useFloatingPosition";

const makeRect = (overrides: Partial<DOMRect> = {}): DOMRect =>
  ({
    x: 0,
    y: 0,
    width: 100,
    height: 30,
    top: 0,
    left: 0,
    right: 100,
    bottom: 30,
    toJSON: () => {},
    ...overrides,
  }) as DOMRect;

const viewport: Viewport = { width: 1920, height: 1080 };

describe("computeFloatingStyle", () => {
  it("positions below and left-aligned by default", () => {
    const rect = makeRect({ top: 200, bottom: 230, left: 300, right: 400 });
    const style = computeFloatingStyle(rect, viewport);

    expect(style.top).toBe(234);
    expect(style.left).toBe(300);
    expect(style.bottom).toBeUndefined();
    expect(style.right).toBeUndefined();
  });

  it("flips to top when not enough space below", () => {
    const rect = makeRect({ top: 950, bottom: 980, left: 100, right: 200 });
    const style = computeFloatingStyle(rect, viewport, { placement: "bottom-start" });

    expect(style.bottom).toBe(1080 - 950 + 4);
    expect(style.top).toBeUndefined();
  });

  it("does not flip when enough space on preferred side", () => {
    const rect = makeRect({ top: 400, bottom: 430, left: 100, right: 200 });
    const style = computeFloatingStyle(rect, viewport, { placement: "bottom-start" });

    expect(style.top).toBe(434);
    expect(style.bottom).toBeUndefined();
  });

  it("caps maxHeight to available space", () => {
    const rect = makeRect({ top: 400, bottom: 430, left: 100, right: 200 });
    const style = computeFloatingStyle(rect, viewport, { maxHeight: 400 });

    expect(style.maxHeight).toBe(400);
    expect(style.overflowY).toBe("auto");
  });

  it("reduces maxHeight when flipped side also lacks space", () => {
    const rect = makeRect({ top: 250, bottom: 280, left: 100, right: 200 });
    const small: Viewport = { width: 1920, height: 400 };
    const style = computeFloatingStyle(rect, small, { maxHeight: 500 });

    // Flips to top: availableSpace = (250 - 8) - 4 = 238
    expect(style.maxHeight).toBe(238);
  });

  it("clamps start alignment at right viewport edge", () => {
    const rect = makeRect({ top: 200, bottom: 230, left: 1800, right: 1900 });
    const style = computeFloatingStyle(rect, viewport, { placement: "bottom-start", minWidth: 240 });

    // 1920 - 240 - 8 = 1672
    expect(style.left).toBe(1672);
  });

  it("right-aligns end placement with trigger", () => {
    const rect = makeRect({ top: 200, bottom: 230, left: 800, right: 900 });
    const style = computeFloatingStyle(rect, viewport, { placement: "bottom-end", minWidth: 320 });

    expect(style.right).toBe(1920 - 900);
  });

  it("clamps end alignment to prevent left-edge overflow", () => {
    const rect = makeRect({ top: 200, bottom: 230, left: 20, right: 120 });
    const style = computeFloatingStyle(rect, viewport, { placement: "bottom-end", minWidth: 320 });

    // 1920 - 320 - 8 = 1592
    expect(style.right).toBe(1592);
  });

  it("centers with translateX(-50%) when minWidth is not provided", () => {
    const rect = makeRect({ top: 200, bottom: 230, left: 500, right: 600, width: 100 });
    const style = computeFloatingStyle(rect, viewport, { placement: "top-center" });

    expect(style.left).toBe(550);
    expect(style.transform).toBe("translateX(-50%)");
  });

  it("clamps center placement with minWidth near viewport edge", () => {
    const rect = makeRect({ top: 200, bottom: 230, left: 1870, right: 1910, width: 40 });
    const style = computeFloatingStyle(rect, viewport, { placement: "top-center", minWidth: 320 });

    // 1920 - 320 - 8 = 1592
    expect(style.left).toBe(1592);
    expect(style.transform).toBeUndefined();
  });
});

const setViewport = (width: number, height: number) => {
  Object.defineProperty(window, "innerWidth", { value: width, writable: true });
  Object.defineProperty(window, "innerHeight", { value: height, writable: true });
};

describe("useFloatingPosition", () => {
  beforeEach(() => {
    setViewport(1920, 1080);
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((cb) => {
      cb(0);
      return 0;
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  const mountWithTrigger = (rect: DOMRect) => {
    const hook = renderHook(() => useFloatingPosition({ placement: "bottom-start" }));
    const element = { getBoundingClientRect: () => rect } as HTMLElement;
    (hook.result.current.triggerRef as MutableRefObject<HTMLElement | null>).current = element;
    return { hook, element };
  };

  it("show is a no-op when triggerRef.current is null", () => {
    const hook = renderHook(() => useFloatingPosition({ placement: "bottom-start" }));

    act(() => hook.result.current.show());

    expect(hook.result.current.isVisible).toBe(false);
    expect(hook.result.current.floatingStyle).toBeUndefined();
  });

  it("updates position when a scroll event fires while visible", () => {
    const initialRect = makeRect({ top: 200, bottom: 230, left: 300, right: 400 });
    const { hook, element } = mountWithTrigger(initialRect);

    act(() => hook.result.current.show());
    expect(hook.result.current.floatingStyle?.top).toBe(234);

    const scrolledRect = makeRect({ top: 100, bottom: 130, left: 300, right: 400 });
    (element as { getBoundingClientRect: () => DOMRect }).getBoundingClientRect = () => scrolledRect;

    act(() => {
      window.dispatchEvent(new Event("scroll"));
    });

    expect(hook.result.current.floatingStyle?.top).toBe(134);
  });

  it("stops tracking after hide is called", () => {
    const initialRect = makeRect({ top: 200, bottom: 230, left: 300, right: 400 });
    const { hook, element } = mountWithTrigger(initialRect);

    act(() => hook.result.current.show());
    act(() => hook.result.current.hide());

    const spy = vi.fn(element.getBoundingClientRect);
    (element as { getBoundingClientRect: () => DOMRect }).getBoundingClientRect = spy;

    act(() => {
      window.dispatchEvent(new Event("scroll"));
    });

    expect(spy).not.toHaveBeenCalled();
    expect(hook.result.current.isVisible).toBe(false);
  });
});
