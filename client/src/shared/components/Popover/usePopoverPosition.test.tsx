import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import usePopoverPosition from "./usePopoverPosition";
import { positions } from "@/shared/constants";

const mocks = vi.hoisted(() => ({
  viewport: { width: 1440, height: 360 },
  measureRef: () => {},
  popoverRect: { x: 0, y: 0, top: 0, left: 0, right: 320, bottom: 280, width: 320, height: 280 },
}));
vi.mock("@/shared/hooks/useWindowDimensions", () => ({ useWindowDimensions: () => mocks.viewport }));
vi.mock("@/shared/hooks/useMeasure", () => ({
  default: () => [mocks.measureRef, mocks.popoverRect, mocks.popoverRect],
}));

beforeEach(() => {
  mocks.viewport = { width: 1440, height: 360 };
  vi.stubGlobal("visualViewport", undefined);
  vi.stubGlobal("scrollY", 0);
});
afterEach(() => vi.unstubAllGlobals());

function fixture(mode: "portal-scrolling" | "portal-fixed", constrainHeight = false) {
  const trigger = document.createElement("div");
  let triggerTop = 160;
  trigger.getBoundingClientRect = () => new DOMRect(100, triggerTop, 40, 56);
  const triggerRef = { current: trigger };
  const hook = renderHook(() =>
    usePopoverPosition(triggerRef, 24, 0, 0, mode, positions.top, false, false, constrainHeight),
  );
  return { ...hook, setTriggerTop: (top: number) => (triggerTop = top) };
}

function visualTop(styleTop: unknown, animation: string, pageOffset = 0) {
  return Number.parseFloat(String(styleTop)) - pageOffset - (animation.includes("slide-up") ? 8 : 0);
}

describe("portal viewport clamping", () => {
  it.each(["portal-scrolling", "portal-fixed"] as const)(
    "keeps %s inside a short viewport when neither side fits",
    (mode) => {
      const { result } = fixture(mode);
      const top = visualTop(result.current.popoverStyle.top, result.current.popoverAnimation);
      expect(top).toBeGreaterThanOrEqual(8);
      expect(top + 280).toBeLessThanOrEqual(352);
    },
  );

  it("reclamps a scrolling popover using document coordinates after page scroll", () => {
    const { result, setTriggerTop } = fixture("portal-scrolling");
    act(() => {
      vi.stubGlobal("scrollY", 120);
      setTriggerTop(40);
      window.dispatchEvent(new Event("scroll"));
    });
    const top = visualTop(result.current.popoverStyle.top, result.current.popoverAnimation, 120);
    expect(top).toBeGreaterThanOrEqual(8);
    expect(top + 280).toBeLessThanOrEqual(352);
  });

  it.each(["portal-scrolling", "portal-fixed"] as const)(
    "caps %s content and reclamps when the viewport shrinks below its height",
    (mode) => {
      const { result, rerender } = fixture(mode, true);
      mocks.viewport = { width: 1440, height: 200 };
      rerender();
      expect(result.current.popoverStyle.maxHeight).toBe("184px");
      expect(visualTop(result.current.popoverStyle.top, result.current.popoverAnimation)).toBe(8);
    },
  );
});
