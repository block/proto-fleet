import { StrictMode, Suspense, useEffect, useLayoutEffect } from "react";
import { act, render, renderHook } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { useMountedCallback } from "./useMountedCallback";

it("uses the current owner's callback and stops after that owner unmounts", () => {
  const initial = vi.fn();
  const current = vi.fn();
  const { result, rerender, unmount } = renderHook(({ callback }) => useMountedCallback(callback), {
    initialProps: { callback: initial },
    wrapper: StrictMode,
  });
  const complete = result.current;
  rerender({ callback: current });
  complete();
  expect(initial).not.toHaveBeenCalled();
  expect(current).toHaveBeenCalledOnce();
  unmount();
  complete();
  expect(current).toHaveBeenCalledOnce();
});

it("accepts callbacks from descendant layout and passive mount effects", () => {
  const callback = vi.fn();
  function Child({ complete }: { complete: () => void }) {
    useLayoutEffect(complete, [complete]);
    useEffect(complete, [complete]);
    return null;
  }
  function Owner() {
    return <Child complete={useMountedCallback(callback)} />;
  }
  render(<Owner />);
  expect(callback).toHaveBeenCalledTimes(2);
});

it("keeps the committed callback when a later render suspends", async () => {
  const initial = vi.fn();
  const uncommitted = vi.fn();
  const pending = new Promise<never>(() => undefined);
  let complete: () => void = () => undefined;
  function Owner({ suspend }: { suspend: boolean }) {
    const callback = useMountedCallback(suspend ? uncommitted : initial);
    useLayoutEffect(() => {
      complete = callback;
    }, [callback]);
    if (suspend) throw pending;
    return null;
  }
  const view = (suspend: boolean) => (
    <Suspense fallback={null}>
      <Owner suspend={suspend} />
    </Suspense>
  );
  const { rerender } = render(view(false));
  await act(async () => rerender(view(true)));
  complete();
  expect(initial).toHaveBeenCalledOnce();
  expect(uncommitted).not.toHaveBeenCalled();
});
