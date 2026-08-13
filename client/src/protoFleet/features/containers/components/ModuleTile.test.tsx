import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { mockUseWindowDimensions } = vi.hoisted(() => ({
  mockUseWindowDimensions: vi.fn(() => ({ isPhone: false })),
}));

vi.mock("@/shared/hooks/useWindowDimensions", () => ({
  useWindowDimensions: mockUseWindowDimensions,
}));

// eslint-disable-next-line import-x/order -- mocked hook must be registered before importing the component
import ModuleTile from "./ModuleTile";

describe("ModuleTile", () => {
  beforeEach(() => {
    mockUseWindowDimensions.mockReturnValue({ isPhone: false });
  });

  it("dispatches a desktop popover action and closes the menu", () => {
    const onAction = vi.fn();
    render(<ModuleTile state="healthy" label="Tank 1 module 1" onAction={onAction} />);

    const trigger = screen.getByRole("button", { name: "Tank 1 module 1 actions" });
    fireEvent.click(trigger);

    expect(trigger).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByTestId("module-actions-popover")).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("module-action-reboot"));

    expect(onAction).toHaveBeenCalledWith("reboot");
    expect(trigger).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByTestId("module-actions-popover")).not.toBeInTheDocument();
  });

  it("dispatches a mobile action-sheet action and closes the sheet", () => {
    mockUseWindowDimensions.mockReturnValue({ isPhone: true });
    const onAction = vi.fn();
    render(<ModuleTile state="attention" label="Tank 1 module 2" onAction={onAction} />);

    const trigger = screen.getByRole("button", { name: "Tank 1 module 2 actions" });
    fireEvent.click(trigger);

    expect(screen.getByTestId("module-actions-popover-sheet")).toBeInTheDocument();
    expect(screen.getByTestId("module-actions-popover-sheet").parentElement).toBe(document.body);

    fireEvent.click(screen.getByTestId("module-action-blink"));

    expect(onAction).toHaveBeenCalledWith("blink");
    expect(trigger).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByTestId("module-actions-popover-sheet")).not.toBeInTheDocument();
  });
});
