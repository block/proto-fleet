import { fireEvent, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { __resetClickOutsideStackForTests, useClickOutsideDismiss } from "./useClickOutsideDismiss";

const makeEl = (id: string) => {
  const el = document.createElement("div");
  el.setAttribute("data-testid", id);
  document.body.appendChild(el);
  return el;
};

const clickOn = (target: Element) => fireEvent.mouseDown(target);

describe("useClickOutsideDismiss", () => {
  let outsideEl: HTMLElement;
  let parentEl: HTMLElement;
  let childEl: HTMLElement;

  beforeEach(() => {
    outsideEl = makeEl("outside");
    parentEl = makeEl("parent");
    childEl = makeEl("child");
  });

  afterEach(() => {
    __resetClickOutsideStackForTests();
    document.body.innerHTML = "";
  });

  it("fires when the click is outside the ref", () => {
    const onDismiss = vi.fn();
    renderHook(() => useClickOutsideDismiss({ ref: { current: parentEl }, onDismiss }));

    clickOn(outsideEl);

    expect(onDismiss).toHaveBeenCalledTimes(1);
  });

  it("does not fire when the click is inside the ref", () => {
    const onDismiss = vi.fn();
    renderHook(() => useClickOutsideDismiss({ ref: { current: parentEl }, onDismiss }));

    clickOn(parentEl);

    expect(onDismiss).not.toHaveBeenCalled();
  });

  it("parent does not dismiss when the click is inside a child layer", () => {
    const parentDismiss = vi.fn();
    const childDismiss = vi.fn();
    renderHook(() => useClickOutsideDismiss({ ref: { current: parentEl }, onDismiss: parentDismiss }));
    renderHook(() => useClickOutsideDismiss({ ref: { current: childEl }, onDismiss: childDismiss }));

    clickOn(childEl);

    expect(parentDismiss).not.toHaveBeenCalled();
    expect(childDismiss).not.toHaveBeenCalled();
  });

  it("click outside child but inside parent dismisses only the child", () => {
    const parentDismiss = vi.fn();
    const childDismiss = vi.fn();
    renderHook(() => useClickOutsideDismiss({ ref: { current: parentEl }, onDismiss: parentDismiss }));
    renderHook(() => useClickOutsideDismiss({ ref: { current: childEl }, onDismiss: childDismiss }));

    clickOn(parentEl);

    expect(childDismiss).toHaveBeenCalledTimes(1);
    expect(parentDismiss).not.toHaveBeenCalled();
  });

  it("click outside both layers dismisses only the topmost", () => {
    const parentDismiss = vi.fn();
    const childDismiss = vi.fn();
    renderHook(() => useClickOutsideDismiss({ ref: { current: parentEl }, onDismiss: parentDismiss }));
    renderHook(() => useClickOutsideDismiss({ ref: { current: childEl }, onDismiss: childDismiss }));

    clickOn(outsideEl);

    expect(childDismiss).toHaveBeenCalledTimes(1);
    expect(parentDismiss).not.toHaveBeenCalled();
  });

  it("does not register when onDismiss is undefined", () => {
    const parentDismiss = vi.fn();
    renderHook(() => useClickOutsideDismiss({ ref: { current: parentEl }, onDismiss: parentDismiss }));
    renderHook(() => useClickOutsideDismiss({ ref: { current: childEl }, onDismiss: undefined }));

    clickOn(childEl);

    expect(parentDismiss).toHaveBeenCalledTimes(1);
  });

  it("honors ignoreSelectors", () => {
    const onDismiss = vi.fn();
    const ignored = makeEl("popover");
    ignored.classList.add("popover-content");
    renderHook(() =>
      useClickOutsideDismiss({
        ref: { current: parentEl },
        onDismiss,
        ignoreSelectors: [".popover-content"],
      }),
    );

    clickOn(ignored);

    expect(onDismiss).not.toHaveBeenCalled();
  });
});
