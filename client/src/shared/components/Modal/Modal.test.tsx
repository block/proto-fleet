import type { HTMLAttributes, ReactNode } from "react";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import Modal from ".";

vi.mock("motion/react", () => ({
  AnimatePresence: ({ children }: { children: ReactNode }) => children,
  motion: {
    div: ({ children, ...props }: HTMLAttributes<HTMLDivElement>) => <div {...props}>{children}</div>,
  },
}));

describe("Modal", () => {
  it("keeps the title expanded at the top and collapsed consistently while scrolling", () => {
    render(
      <Modal title="Channel settings">
        <div>Scrollable settings</div>
      </Modal>,
    );

    const scrollArea = screen.getByTestId("modal");
    const header = screen.getByRole("button", { name: "Close dialog" }).closest<HTMLElement>(".sticky")!;

    expect(within(header).queryByText("Channel settings")).not.toBeInTheDocument();
    expect(screen.getByText("Channel settings")).toBeInTheDocument();

    for (const scrollTop of [1, 1, 100, 100, 1]) {
      fireEvent.scroll(scrollArea, { target: { scrollTop } });
      expect(within(header).getByText("Channel settings")).toBeInTheDocument();
    }

    fireEvent.scroll(scrollArea, { target: { scrollTop: 0 } });
    expect(within(header).queryByText("Channel settings")).not.toBeInTheDocument();
    expect(screen.getByText("Channel settings")).toBeInTheDocument();
  });

  it("tracks scrolling when first opened and resets the title when reopened", () => {
    const modal = (open: boolean) => (
      <Modal title="Channel settings" open={open}>
        <div>Scrollable settings</div>
      </Modal>
    );
    const { rerender } = render(modal(false));
    expect(screen.queryByTestId("modal")).not.toBeInTheDocument();

    rerender(modal(true));
    const firstHeader = screen.getByRole("button", { name: "Close dialog" }).closest<HTMLElement>(".sticky")!;
    expect(within(firstHeader).queryByText("Channel settings")).not.toBeInTheDocument();

    fireEvent.scroll(screen.getByTestId("modal"), { target: { scrollTop: 100 } });
    expect(within(firstHeader).getByText("Channel settings")).toBeInTheDocument();

    rerender(modal(false));
    expect(screen.queryByTestId("modal")).not.toBeInTheDocument();
    rerender(modal(true));

    const reopenedHeader = screen.getByRole("button", { name: "Close dialog" }).closest<HTMLElement>(".sticky")!;
    expect(within(reopenedHeader).queryByText("Channel settings")).not.toBeInTheDocument();
    expect(screen.getByText("Channel settings")).toBeInTheDocument();

    fireEvent.scroll(screen.getByTestId("modal"), { target: { scrollTop: 1 } });
    expect(within(reopenedHeader).getByText("Channel settings")).toBeInTheDocument();
  });

  it.each([
    { name: "forced", props: { forceTitleCollapsed: true } },
    { name: "fullscreen", props: { size: "fullscreen" as const } },
  ])("keeps the $name title in the header regardless of scroll position", ({ props }) => {
    render(
      <Modal title="Persistent title" {...props}>
        <div>Scrollable content</div>
      </Modal>,
    );

    const header = screen.getByRole("button", { name: "Close dialog" }).closest<HTMLElement>(".sticky")!;
    expect(within(header).getByText("Persistent title")).toBeInTheDocument();
    expect(screen.getAllByText("Persistent title")).toHaveLength(1);

    for (const scrollTop of [100, 0]) {
      fireEvent.scroll(screen.getByTestId("modal"), { target: { scrollTop } });
      expect(within(header).getByText("Persistent title")).toBeInTheDocument();
      expect(screen.getAllByText("Persistent title")).toHaveLength(1);
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

  it("renders a leading header action before the desktop button group", () => {
    render(
      <Modal
        title="Rollout details"
        forceTitleCollapsed
        headerLeadingAction={<button type="button">More</button>}
        buttons={[{ text: "Manage", variant: "secondary", onClick: vi.fn() }]}
      >
        <div>Rollout content</div>
      </Modal>,
    );

    const more = screen.getByRole("button", { name: "More" });
    const manage = screen.getByRole("button", { name: "Manage" });
    expect(more.compareDocumentPosition(manage) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
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
