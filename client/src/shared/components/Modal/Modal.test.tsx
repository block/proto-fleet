import type { HTMLAttributes, ReactNode } from "react";
import { render, screen } from "@testing-library/react";
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
});
