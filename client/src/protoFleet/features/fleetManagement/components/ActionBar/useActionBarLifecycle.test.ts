import { act, renderHook } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";
import useActionBarLifecycle from "./useActionBarLifecycle";

const { setActionBarVisible } = vi.hoisted(() => ({ setActionBarVisible: vi.fn() }));

vi.mock("@/protoFleet/store", () => ({
  useSetActionBarVisible: () => setActionBarVisible,
}));

beforeEach(() => vi.clearAllMocks());

test("keeps the toaster offset cleared while busy and restores it for the latest selection", () => {
  const onActionBusyChange = vi.fn();
  const { result, rerender } = renderHook(({ count }) => useActionBarLifecycle(count, onActionBusyChange), {
    initialProps: { count: 2 },
  });
  expect(setActionBarVisible).toHaveBeenLastCalledWith(true);

  act(() => result.current.onActionStart());
  expect(result.current.isActionBusy).toBe(true);
  expect(onActionBusyChange).toHaveBeenLastCalledWith(true);
  expect(setActionBarVisible).toHaveBeenLastCalledWith(false);

  rerender({ count: 3 });
  expect(setActionBarVisible).toHaveBeenLastCalledWith(false);

  act(() => result.current.onActionComplete());
  expect(result.current.isActionBusy).toBe(false);
  expect(onActionBusyChange).toHaveBeenLastCalledWith(false);
  expect(setActionBarVisible).toHaveBeenLastCalledWith(true);
});

test("does not restore the toaster offset when selection clears during an action", () => {
  const { result, rerender } = renderHook(({ count }) => useActionBarLifecycle(count), {
    initialProps: { count: 2 },
  });
  act(() => result.current.onActionStart());
  const complete = result.current.onActionComplete;
  rerender({ count: 0 });

  act(complete);
  expect(setActionBarVisible).toHaveBeenLastCalledWith(false);
});

test("cleans up on navigation and ignores a late completion", () => {
  const onActionBusyChange = vi.fn();
  const { result, unmount } = renderHook(() => useActionBarLifecycle(2, onActionBusyChange));
  act(() => result.current.onActionStart());
  const complete = result.current.onActionComplete;

  unmount();
  expect(setActionBarVisible).toHaveBeenLastCalledWith(false);
  expect(onActionBusyChange).toHaveBeenLastCalledWith(false);
  vi.clearAllMocks();

  act(complete);
  expect(setActionBarVisible).not.toHaveBeenCalled();
  expect(onActionBusyChange).not.toHaveBeenCalled();
});

test.each([false, true])("changing the busy callback does not reset the lifecycle (busy: %s)", (busy) => {
  const first = vi.fn();
  const latest = vi.fn();
  const { result, rerender, unmount } = renderHook(({ onBusyChange }) => useActionBarLifecycle(2, onBusyChange), {
    initialProps: { onBusyChange: first },
  });
  if (busy) act(() => result.current.onActionStart());
  const complete = result.current.onActionComplete;
  vi.clearAllMocks();

  rerender({ onBusyChange: latest });
  expect(result.current.isActionBusy).toBe(busy);
  expect(first).not.toHaveBeenCalled();
  expect(latest).not.toHaveBeenCalled();
  expect(setActionBarVisible).not.toHaveBeenCalled();

  act(complete);
  expect(latest).toHaveBeenLastCalledWith(false);
  expect(first).not.toHaveBeenCalled();
  vi.clearAllMocks();
  unmount();
  expect(latest).toHaveBeenCalledWith(false);
  expect(setActionBarVisible).toHaveBeenCalledWith(false);
});
