import type { HTMLAttributes, ReactNode } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import FullScreenTwoPaneModal from "./FullScreenTwoPaneModal";
import { variants } from "@/shared/components/Button";

const mockUseWindowDimensions = vi.fn();

vi.mock("motion/react", () => ({
  AnimatePresence: ({ children }: { children: ReactNode }) => children,
  motion: {
    div: ({ children, ...props }: HTMLAttributes<HTMLDivElement>) => <div {...props}>{children}</div>,
  },
}));

vi.mock("@/shared/hooks/useWindowDimensions", () => ({
  useWindowDimensions: () => mockUseWindowDimensions(),
}));

const renderModal = () =>
  render(
    <FullScreenTwoPaneModal
      open
      title="Rack"
      buttons={[
        { text: "Delete Rack", variant: variants.secondaryDanger },
        { text: "Edit Rack Settings", variant: variants.secondary },
        { text: "Manage Miners", variant: variants.secondary },
        { text: "Save", variant: variants.primary },
      ]}
      primaryPane={<div>Primary pane</div>}
      secondaryPane={<div>Secondary pane</div>}
    />,
  );

describe("FullScreenTwoPaneModal", () => {
  beforeEach(() => {
    mockUseWindowDimensions.mockReturnValue({
      height: 852,
      width: 632,
      isDesktop: false,
      isLaptop: false,
      isTablet: true,
      isPhone: false,
    });
  });

  it("uses compact header actions on tablets", () => {
    renderModal();

    expect(screen.getByRole("button", { name: "More actions" })).toBeVisible();
    expect(screen.getByRole("button", { name: "Save" })).toBeVisible();
    expect(screen.queryByRole("button", { name: "Edit Rack Settings" })).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "More actions" }));

    expect(screen.getByTestId("modal-overflow-sheet")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Edit Rack Settings" })).toBeVisible();
    expect(screen.getByRole("button", { name: "Manage Miners" })).toBeVisible();
  });

  it("keeps the full action row on laptop and wider viewports", () => {
    mockUseWindowDimensions.mockReturnValue({
      height: 900,
      width: 960,
      isDesktop: false,
      isLaptop: true,
      isTablet: false,
      isPhone: false,
    });

    renderModal();

    expect(screen.queryByRole("button", { name: "More actions" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Delete Rack" })).toBeVisible();
    expect(screen.getByRole("button", { name: "Edit Rack Settings" })).toBeVisible();
    expect(screen.getByRole("button", { name: "Manage Miners" })).toBeVisible();
    expect(screen.getByRole("button", { name: "Save" })).toBeVisible();
  });
});
