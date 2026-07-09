import { render, screen } from "@testing-library/react";
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
});
