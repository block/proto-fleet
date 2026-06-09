import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import RowActionsMenu from "./RowActionsMenu";

describe("RowActionsMenu", () => {
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
});
