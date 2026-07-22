import type { HTMLAttributes, ReactNode } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import Modal from ".";

vi.mock("motion/react", () => ({
  AnimatePresence: ({ children }: { children: ReactNode }) => children,
  motion: {
    div: ({ children, ...props }: HTMLAttributes<HTMLDivElement>) => <div {...props}>{children}</div>,
  },
}));

describe("Modal", () => {
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

  it("caps fullscreen modal width by default while allowing opt out", () => {
    const { rerender } = render(
      <Modal title="Fullscreen modal" size="fullscreen">
        <div>Fullscreen content</div>
      </Modal>,
    );

    expect(screen.getByTestId("modal").parentElement).toHaveStyle({ maxWidth: "1920px" });

    rerender(
      <Modal title="Fullscreen modal" size="fullscreen" fullscreenMaxWidth={false}>
        <div>Fullscreen content</div>
      </Modal>,
    );

    expect(screen.getByTestId("modal").parentElement).not.toHaveStyle({ maxWidth: "1920px" });
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
