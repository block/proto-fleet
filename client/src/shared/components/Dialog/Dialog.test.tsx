import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import Dialog from "./Dialog";
import { variants } from "@/shared/components/Button";

describe("Dialog", () => {
  it("keeps two short actions horizontal on mobile while flexing buttons to fill the row", () => {
    render(
      <Dialog
        title="Short actions"
        buttons={[
          { text: "Cancel", variant: variants.secondary, onClick: vi.fn() },
          { text: "Save", variant: variants.primary, onClick: vi.fn() },
        ]}
      />,
    );

    const cancel = screen.getByRole("button", { name: "Cancel" });
    const group = cancel.parentElement;

    expect(group).not.toHaveClass("flex-col");
    expect(group).toHaveClass("gap-3");
    expect(cancel).not.toHaveClass("phone:w-full");
    expect(cancel).not.toHaveClass("phone:order-3");
    expect(cancel).toHaveClass("phone:flex-1");

    const save = screen.getByRole("button", { name: "Save" });

    expect(save).not.toHaveClass("phone:w-full");
    expect(save).not.toHaveClass("phone:order-1");
    expect(save).toHaveClass("phone:flex-1");
  });

  it("stacks two actions when either label is long", () => {
    render(
      <Dialog
        title="Long actions"
        buttons={[
          { text: "Cancel", variant: variants.secondary, onClick: vi.fn() },
          { text: "Take photo instead", variant: variants.primary, onClick: vi.fn() },
        ]}
      />,
    );

    const buttons = screen.getAllByRole("button");

    expect(buttons.map((button) => button.textContent)).toEqual(["Take photo instead", "Cancel"]);
    expect(buttons[0].parentElement).toHaveClass("flex-col");
    expect(buttons[0].parentElement).toHaveClass("gap-3");
    expect(buttons[0]).toHaveClass("w-full");
    expect(buttons[0]).toHaveClass("phone:order-1");
    expect(buttons[1]).toHaveClass("w-full");
    expect(buttons[1]).toHaveClass("phone:order-3");
  });

  it("always stacks three actions with primary, secondary, then close action", () => {
    render(
      <Dialog
        title="Three actions"
        buttons={[
          { text: "Dismiss", variant: variants.secondary, onClick: vi.fn() },
          { text: "Undo", variant: variants.secondary, onClick: vi.fn() },
          { text: "Scan next slot", variant: variants.primary, onClick: vi.fn() },
        ]}
      />,
    );

    const buttons = screen.getAllByRole("button");

    expect(buttons.map((button) => button.textContent)).toEqual(["Scan next slot", "Undo", "Dismiss"]);
    expect(buttons[0].parentElement).toHaveClass("flex-col");
    expect(buttons[0].parentElement).toHaveClass("gap-3");
    expect(buttons[0]).toHaveClass("phone:order-1");
    expect(buttons[1]).toHaveClass("phone:order-2");
    expect(buttons[2]).toHaveClass("phone:order-3");
    expect(buttons.every((button) => button.className.includes("w-full"))).toBe(true);
  });
});
