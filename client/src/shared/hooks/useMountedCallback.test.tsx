import { StrictMode } from "react";
import { renderHook } from "@testing-library/react";
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
