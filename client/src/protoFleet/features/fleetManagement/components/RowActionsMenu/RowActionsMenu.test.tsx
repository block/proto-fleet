import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { mockUseWindowDimensions } = vi.hoisted(() => ({
  mockUseWindowDimensions: vi.fn(() => ({ isPhone: false })),
}));

vi.mock("@/shared/hooks/useWindowDimensions", () => ({
  useWindowDimensions: mockUseWindowDimensions,
}));

// eslint-disable-next-line import-x/order -- mocked hook must be registered before importing the component
import RowActionsMenu from "./RowActionsMenu";

describe("RowActionsMenu", () => {
  beforeEach(() => {
    mockUseWindowDimensions.mockReturnValue({ isPhone: false });
  });

  it("opens on trigger click and renders all visible actions", () => {
    render(
      <RowActionsMenu
        actions={[
          { label: "Edit", onClick: vi.fn() },
          { label: "Delete", onClick: vi.fn() },
        ]}
      />,
    );
    expect(screen.queryByText("Edit")).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId("row-actions-menu-trigger"));
    expect(screen.getByText("Edit")).toBeInTheDocument();
    expect(screen.getByText("Delete")).toBeInTheDocument();
  });

  it("fires the action handler and closes the popover", () => {
    const onEdit = vi.fn();
    render(
      <RowActionsMenu
        actions={[
          { label: "Edit", onClick: onEdit },
          { label: "Delete", onClick: vi.fn() },
        ]}
      />,
    );
    fireEvent.click(screen.getByTestId("row-actions-menu-trigger"));
    fireEvent.click(screen.getByText("Edit"));
    expect(onEdit).toHaveBeenCalledTimes(1);
    expect(screen.queryByText("Edit")).not.toBeInTheDocument();
  });

  it("omits hidden actions from the popover", () => {
    render(
      <RowActionsMenu
        actions={[
          { label: "Edit", onClick: vi.fn() },
          { label: "Delete", onClick: vi.fn(), hidden: true },
        ]}
      />,
    );
    fireEvent.click(screen.getByTestId("row-actions-menu-trigger"));
    expect(screen.getByText("Edit")).toBeInTheDocument();
    expect(screen.queryByText("Delete")).not.toBeInTheDocument();
  });

  it("renders nothing when every action is hidden", () => {
    render(
      <RowActionsMenu
        actions={[
          { label: "Edit", onClick: vi.fn(), hidden: true },
          { label: "Delete", onClick: vi.fn(), hidden: true },
        ]}
      />,
    );
    expect(screen.queryByTestId("row-actions-menu-trigger")).not.toBeInTheDocument();
  });

  it("honors a custom testIdPrefix on the trigger and popover", () => {
    render(<RowActionsMenu actions={[{ label: "Edit", onClick: vi.fn() }]} testIdPrefix="my-row-actions" />);
    expect(screen.getByTestId("my-row-actions-trigger")).toBeInTheDocument();
  });

  it("renders mobile menus through the shared action sheet without the desktop popover padding", () => {
    mockUseWindowDimensions.mockReturnValue({ isPhone: true });

    render(
      <RowActionsMenu
        actions={[
          {
            label: "View racks",
            icon: <span data-testid="view-racks-icon" />,
            onClick: vi.fn(),
            showGroupDivider: true,
            testId: "view-racks-action",
          },
          { label: "Edit site", onClick: vi.fn(), testId: "edit-site-action" },
        ]}
      />,
    );

    fireEvent.click(screen.getByTestId("row-actions-menu-trigger"));

    expect(screen.getByTestId("row-actions-menu-popover-sheet")).toBeInTheDocument();
    expect(screen.getByTestId("row-actions-menu-popover-sheet").parentElement).toBe(document.body);
    expect(screen.getByTestId("row-actions-menu-popover")).toHaveClass("rounded-t-2xl", "px-6");
    expect(screen.getByTestId("row-actions-menu-popover")).not.toHaveClass("p-6");
    expect(screen.getByTestId("view-racks-icon")).toBeInTheDocument();
    expect(screen.getByTestId("view-racks-action")).toHaveTextContent("View racks");
  });
});
