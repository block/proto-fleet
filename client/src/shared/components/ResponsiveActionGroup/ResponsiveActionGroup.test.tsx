import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import ResponsiveActionGroup from "./ResponsiveActionGroup";
import { sizes, variants } from "@/shared/components/Button";

describe("ResponsiveActionGroup", () => {
  it("keeps the right-most action visible and moves earlier actions into an action sheet", () => {
    const onManageColumns = vi.fn();
    const onExport = vi.fn();
    const onAddMiners = vi.fn();

    render(
      <ResponsiveActionGroup
        buttons={[
          {
            actionSheetLabel: "Manage columns",
            ariaLabel: "Manage columns",
            onClick: onManageColumns,
            variant: variants.secondary,
          },
          {
            onClick: onExport,
            text: "Export CSV",
            variant: variants.secondary,
          },
          {
            onClick: onAddMiners,
            text: "Add miners",
            variant: variants.secondary,
          },
        ]}
        primaryButtonStrategy="last"
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "More actions" }));

    expect(screen.getByRole("button", { name: "Add miners" })).toBeInTheDocument();
    expect(screen.getByTestId("responsive-action-sheet")).toBeInTheDocument();
    expect(screen.getByText("Manage columns")).toBeInTheDocument();
    expect(screen.getByText("Export CSV")).toBeInTheDocument();

    fireEvent.click(screen.getByText("Export CSV"));

    expect(onExport).toHaveBeenCalledTimes(1);
    expect(onManageColumns).not.toHaveBeenCalled();
    expect(onAddMiners).not.toHaveBeenCalled();
  });

  it("renders the overflow trigger as a true compact icon button when requested", () => {
    render(
      <ResponsiveActionGroup
        buttonSize={sizes.compact}
        buttons={[
          {
            actionSheetLabel: "Manage columns",
            ariaLabel: "Manage columns",
            onClick: vi.fn(),
            variant: variants.secondary,
          },
          {
            text: "Add miners",
            variant: variants.secondary,
          },
        ]}
      />,
    );

    expect(screen.getByRole("button", { name: "More actions" })).toHaveClass("!h-8", "!w-8", "!px-0", "!py-0");
  });

  it("portals the action sheet to the body so the overlay is not clipped by page containers", () => {
    const host = document.createElement("div");
    document.body.append(host);

    try {
      render(
        <div className="relative overflow-hidden">
          <ResponsiveActionGroup
            buttons={[
              {
                text: "Export CSV",
                variant: variants.secondary,
              },
              {
                text: "Add miners",
                variant: variants.secondary,
              },
            ]}
          />
        </div>,
        { container: host },
      );

      fireEvent.click(screen.getByRole("button", { name: "More actions" }));

      expect(screen.getByTestId("responsive-action-sheet").parentElement).toBe(document.body);
    } finally {
      host.remove();
    }
  });
});
