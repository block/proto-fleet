import { fireEvent, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { __resetEscapeStackForTests, useEscapeDismiss } from "./useEscapeDismiss";

const pressEscape = () => fireEvent.keyDown(document, { key: "Escape" });

describe("useEscapeDismiss", () => {
  afterEach(() => {
    __resetEscapeStackForTests();
  });

  it("fires the handler on Escape", () => {
    const onDismiss = vi.fn();
    renderHook(() => useEscapeDismiss(onDismiss));

    pressEscape();

    expect(onDismiss).toHaveBeenCalledTimes(1);
  });

  it("only fires the topmost frame when multiple are active", () => {
    const outer = vi.fn();
    const inner = vi.fn();
    renderHook(() => useEscapeDismiss(outer));
    const innerHook = renderHook(() => useEscapeDismiss(inner));

    pressEscape();

    expect(inner).toHaveBeenCalledTimes(1);
    expect(outer).not.toHaveBeenCalled();

    innerHook.unmount();
    pressEscape();

    expect(outer).toHaveBeenCalledTimes(1);
    expect(inner).toHaveBeenCalledTimes(1);
  });

  it("does not register when onDismiss is undefined", () => {
    const outer = vi.fn();
    renderHook(() => useEscapeDismiss(outer));
    renderHook(() => useEscapeDismiss(undefined));

    pressEscape();

    expect(outer).toHaveBeenCalledTimes(1);
  });

  it("uses the latest handler without re-subscribing", () => {
    const first = vi.fn();
    const second = vi.fn();
    const { rerender } = renderHook(({ handler }: { handler: () => void }) => useEscapeDismiss(handler), {
      initialProps: { handler: first },
    });

    rerender({ handler: second });
    pressEscape();

    expect(first).not.toHaveBeenCalled();
    expect(second).toHaveBeenCalledTimes(1);
  });
});
