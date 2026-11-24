import { fireEvent, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { useClickOutside } from "./useClickOutside";

const makeElement = () => {
  const element = document.createElement("div");
  document.body.appendChild(element);
  return element;
};

describe("useClickOutside", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    document.body.innerHTML = "";
  });

  it("only registers outside-click listeners while enabled", () => {
    const inside = makeElement();
    const outside = makeElement();
    const onClickOutside = vi.fn();
    const addEventListener = vi.spyOn(document, "addEventListener");
    const removeEventListener = vi.spyOn(document, "removeEventListener");
    const { rerender } = renderHook(
      ({ enabled }) => useClickOutside({ ref: { current: inside }, onClickOutside, enabled }),
      { initialProps: { enabled: false } },
    );

    expect(addEventListener).not.toHaveBeenCalledWith("mousedown", expect.any(Function));
    expect(addEventListener).not.toHaveBeenCalledWith("touchstart", expect.any(Function));
    fireEvent.mouseDown(outside);
    expect(onClickOutside).not.toHaveBeenCalled();

    rerender({ enabled: true });
    expect(addEventListener).toHaveBeenCalledWith("mousedown", expect.any(Function));
    expect(addEventListener).toHaveBeenCalledWith("touchstart", expect.any(Function));
    fireEvent.mouseDown(outside);
    expect(onClickOutside).toHaveBeenCalledTimes(1);

    rerender({ enabled: false });
    expect(removeEventListener).toHaveBeenCalledWith("mousedown", expect.any(Function));
    expect(removeEventListener).toHaveBeenCalledWith("touchstart", expect.any(Function));
    fireEvent.mouseDown(outside);
    expect(onClickOutside).toHaveBeenCalledTimes(1);
  });
});
