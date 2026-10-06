import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useWindowDimensions } from "./useWindowDimensions";

vi.mock("./useCssVariable", () => {
  const breakpoints: Record<string, string> = {
    "--phone-max-width": "631",
    "--tablet-max-width": "959",
    "--laptop-max-width": "1279",
  };
  return { default: (name: string) => breakpoints[name] };
});

afterEach(() => vi.unstubAllGlobals());

describe("useWindowDimensions", () => {
  it("reads the current viewport when remounted after an unobserved resize", () => {
    vi.stubGlobal("innerWidth", 800);
    const first = renderHook(useWindowDimensions);
    expect(first.result.current.isTablet).toBe(true);
    first.unmount();

    vi.stubGlobal("innerWidth", 1000);
    const second = renderHook(useWindowDimensions);
    expect(second.result.current.isTablet).toBe(false);
    expect(second.result.current.isLaptop).toBe(true);
    expect(second.result.current.width).toBe(1000);
  });

  it("updates the breakpoint and height when the viewport resizes", () => {
    vi.stubGlobal("innerWidth", 1000);
    const { result } = renderHook(useWindowDimensions);
    act(() => {
      vi.stubGlobal("innerWidth", 600);
      vi.stubGlobal("innerHeight", 700);
      window.dispatchEvent(new Event("resize"));
    });
    expect(result.current.isPhone).toBe(true);
    expect(result.current.isLaptop).toBe(false);
    expect(result.current.height).toBe(700);
  });
});
