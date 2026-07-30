import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import Popover, { PopoverProvider, usePopover } from ".";

const setViewport = (width: number) => {
  document.body.style.setProperty("--phone-max-width", "631");
  document.body.style.setProperty("--tablet-max-width", "959");
  document.body.style.setProperty("--laptop-max-width", "1279");
  Object.defineProperty(window, "innerWidth", { configurable: true, writable: true, value: width });
  Object.defineProperty(window, "innerHeight", { configurable: true, writable: true, value: 800 });
  window.dispatchEvent(new Event("resize"));
};

const PopoverFixture = ({ onClose = vi.fn() }: { onClose?: () => void }) => {
  const { triggerRef } = usePopover();

  return (
    <div ref={triggerRef}>
      <button type="button">Trigger</button>
      <Popover testId="example-popover" closePopover={onClose}>
        Popover content
      </Popover>
    </div>
  );
};

const OwnerManagedPopoverFixture = () => {
  const { triggerRef } = usePopover();

  return (
    <div ref={triggerRef}>
      <button type="button">Trigger</button>
      <Popover testId="example-popover">Popover content</Popover>
    </div>
  );
};

describe("Popover", () => {
  it("renders as a bottom sheet on phone viewports", () => {
    setViewport(390);

    render(
      <PopoverProvider>
        <PopoverFixture />
      </PopoverProvider>,
    );

    expect(screen.getByTestId("example-popover-sheet")).toBeInTheDocument();
    expect(screen.getByText("Popover content")).toBeInTheDocument();
  });

  it("keeps phone sheet pointer-down events from reaching parent dismiss handlers", () => {
    setViewport(390);
    const onClose = vi.fn();
    const parentDismiss = vi.fn();
    document.addEventListener("mousedown", parentDismiss);
    document.addEventListener("touchstart", parentDismiss);

    try {
      render(
        <PopoverProvider>
          <PopoverFixture onClose={onClose} />
        </PopoverProvider>,
      );

      const sheet = screen.getByTestId("example-popover-sheet");
      fireEvent.mouseDown(sheet);
      fireEvent.touchStart(sheet);

      expect(parentDismiss).not.toHaveBeenCalled();

      fireEvent.click(sheet);

      expect(onClose).toHaveBeenCalledTimes(1);
    } finally {
      document.removeEventListener("mousedown", parentDismiss);
      document.removeEventListener("touchstart", parentDismiss);
    }
  });

  it("lets owner-managed phone sheet backdrops fall back to outside-click dismissal", () => {
    setViewport(390);
    const ownerDismiss = vi.fn();
    document.addEventListener("mousedown", ownerDismiss);
    document.addEventListener("touchstart", ownerDismiss);

    try {
      render(
        <PopoverProvider>
          <OwnerManagedPopoverFixture />
        </PopoverProvider>,
      );

      const sheet = screen.getByTestId("example-popover-sheet");
      fireEvent.mouseDown(sheet);
      fireEvent.touchStart(sheet);

      expect(ownerDismiss).toHaveBeenCalledTimes(2);

      ownerDismiss.mockClear();
      fireEvent.mouseDown(screen.getByTestId("example-popover"));
      fireEvent.touchStart(screen.getByTestId("example-popover"));

      expect(ownerDismiss).not.toHaveBeenCalled();
    } finally {
      document.removeEventListener("mousedown", ownerDismiss);
      document.removeEventListener("touchstart", ownerDismiss);
    }
  });
});
