import type { HTMLAttributes, ReactNode } from "react";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import Modal from ".";

vi.mock("motion/react", () => ({
  AnimatePresence: ({ children }: { children: ReactNode }) => children,
  motion: {
    div: ({ children, ...props }: HTMLAttributes<HTMLDivElement>) => <div {...props}>{children}</div>,
  },
}));

describe("Modal", () => {
  it("includes editable fields in the optional focus trap", () => {
    const rects = vi
      .spyOn(HTMLElement.prototype, "getClientRects")
      .mockReturnValue(Object.assign([new DOMRect()], { item: () => null }));
    try {
      const { unmount } = render(
        <Modal
          open
          manageFocus
          title="Settings"
          dialogLabel="Settings"
          buttons={[{ text: "Apply", variant: "primary" }]}
        >
          <input aria-label="Setpoint" />
        </Modal>,
      );
      const close = screen.getByRole("button", { name: "Close dialog" });
      const apply = screen.getByRole("button", { name: "Apply" });
      expect(close).toHaveFocus();
      apply.focus();
      expect(fireEvent.keyDown(apply, { key: "Tab" })).toBe(true);
      const input = screen.getByLabelText("Setpoint");
      input.focus();
      fireEvent.keyDown(input, { key: "Tab" });
      expect(close).toHaveFocus();
      fireEvent.keyDown(close, { key: "Tab", shiftKey: true });
      expect(input).toHaveFocus();
      unmount();
    } finally {
      rects.mockRestore();
    }
  });
  it("uses the full-width top dialog mobile pattern by default", () => {
    render(
      <Modal title="Standard modal">
        <div>Standard content</div>
      </Modal>,
    );

    expect(screen.getByTestId("modal").parentElement).toHaveClass(
      "phone:mt-10",
      "phone:w-screen",
      "phone:min-w-[100vw]",
    );
    expect(screen.getByTestId("modal").parentElement).not.toHaveClass("phone:mt-auto");
  });

  it("preserves bottom-docked phone sheets when requested", () => {
    render(
      <Modal title="Sheet modal" phoneSheet>
        <div>Sheet content</div>
      </Modal>,
    );

    expect(screen.getByTestId("modal").parentElement).toHaveClass(
      "phone:mt-auto",
      "phone:mb-3",
      "phone:w-[calc(100vw-theme(spacing.6))]",
      "phone:min-w-[calc(100vw-theme(spacing.6))]",
    );
    expect(screen.getByTestId("modal").parentElement).not.toHaveClass("phone:mt-10", "phone:w-screen");
  });

  it("caps fullscreen modal width", () => {
    render(
      <Modal title="Fullscreen modal" size="fullscreen">
        <div>Fullscreen content</div>
      </Modal>,
    );

    expect(screen.getByTestId("modal").parentElement).toHaveStyle({ maxWidth: "1920px" });
  });

  it("dismisses from the close button, not from scrim clicks", () => {
    const onDismiss = vi.fn();
    render(
      <Modal title="Intentional dismiss" onDismiss={onDismiss}>
        <div>Modal content</div>
      </Modal>,
    );

    const overlay = screen.getByTestId("modal").parentElement?.parentElement;
    expect(overlay).not.toBeNull();

    fireEvent.mouseDown(overlay!);
    expect(onDismiss).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Close dialog" }));
    expect(onDismiss).toHaveBeenCalledOnce();
  });

  it("still supports Escape dismissal", () => {
    const onDismiss = vi.fn();
    render(
      <Modal title="Keyboard dismiss" onDismiss={onDismiss}>
        <div>Modal content</div>
      </Modal>,
    );

    fireEvent.keyDown(document, { key: "Escape" });

    expect(onDismiss).toHaveBeenCalledOnce();
  });

  it("keeps a fixed footer outside the scroll area while preserving the standard sticky header", () => {
    const onDismiss = vi.fn();
    render(
      <Modal
        title="Selection modal"
        onDismiss={onDismiss}
        fixedFooter={
          <button type="button" data-testid="fixed-footer">
            Select all
          </button>
        }
      >
        <div>Scrollable content</div>
      </Modal>,
    );

    const scrollArea = screen.getByTestId("modal");
    const fixedFooter = screen.getByTestId("fixed-footer");

    expect(fixedFooter.parentElement?.parentElement).toBe(scrollArea.parentElement);
    expect(scrollArea).not.toContainElement(fixedFooter);
    expect(screen.getByRole("button", { name: "Close dialog" }).closest(".sticky")).not.toBeNull();

    fireEvent.mouseDown(fixedFooter);
    fireEvent.click(fixedFooter);
    expect(onDismiss).not.toHaveBeenCalled();
  });
});

describe("Modal scroll title", () => {
  let reportIntersection: (isIntersecting: boolean) => void;
  const observe = vi.fn();
  const disconnect = vi.fn();

  beforeEach(() => {
    observe.mockClear();
    disconnect.mockClear();
    vi.stubGlobal(
      "IntersectionObserver",
      class {
        constructor(callback: (entries: IntersectionObserverEntry[]) => void) {
          reportIntersection = (isIntersecting) => callback([{ isIntersecting } as IntersectionObserverEntry]);
        }
        observe = observe;
        disconnect = disconnect;
      },
    );
  });
  afterEach(() => vi.unstubAllGlobals());

  it("keeps the compact title hidden at the top despite intersection boundary jitter", () => {
    render(<Modal title="Miner status">Status content</Modal>);
    for (const isIntersecting of [false, true, false]) {
      act(() => reportIntersection(isIntersecting));
      expect(screen.getAllByText("Miner status")).toHaveLength(1);
    }
  });

  it("shows the compact title after scrolling past the large title and clears it immediately at the top", () => {
    render(<Modal title="Miner status">Status content</Modal>);
    const scroller = screen.getByTestId("modal");
    fireEvent.scroll(scroller, { target: { scrollTop: 100 } });
    act(() => reportIntersection(false));
    expect(screen.getAllByText("Miner status")).toHaveLength(2);
    // IntersectionObserver callbacks arrive asynchronously after a scroll event.
    fireEvent.scroll(scroller, { target: { scrollTop: 0 } });
    expect(screen.getAllByText("Miner status")).toHaveLength(1);
    act(() => reportIntersection(false));
    expect(screen.getAllByText("Miner status")).toHaveLength(1);
  });

  it("starts observing when a previously closed modal opens", () => {
    const { rerender } = render(
      <Modal title="Miner status" open={false}>
        Status content
      </Modal>,
    );
    expect(observe).not.toHaveBeenCalled();
    rerender(
      <Modal title="Miner status" open>
        {" "}
        Status content
      </Modal>,
    );
    expect(observe).toHaveBeenCalledOnce();
    act(() => reportIntersection(false));
    expect(screen.getAllByText("Miner status")).toHaveLength(1);
  });
});
