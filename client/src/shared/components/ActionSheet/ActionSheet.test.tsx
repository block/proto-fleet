import { type ReactNode, useRef } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import ActionSheet from "./ActionSheet";
import { __resetClickOutsideStackForTests, useClickOutsideDismiss } from "@/shared/hooks/useClickOutsideDismiss";

const ParentDismissLayer = ({ children, onDismiss }: { children?: ReactNode; onDismiss: () => void }) => {
  const ref = useRef<HTMLDivElement>(null);
  useClickOutsideDismiss({ ref, onDismiss });

  return <div ref={ref}>{children}</div>;
};

describe("ActionSheet", () => {
  beforeEach(() => {
    __resetClickOutsideStackForTests();
  });

  afterEach(() => {
    __resetClickOutsideStackForTests();
  });

  it("dismisses as the top outside-click layer without dismissing its parent layer", () => {
    const parentDismiss = vi.fn();
    const sheetDismiss = vi.fn();

    const { rerender } = render(<ParentDismissLayer onDismiss={parentDismiss} />);

    rerender(
      <ParentDismissLayer onDismiss={parentDismiss}>
        <ActionSheet items={[{ label: "Search miners" }]} onClose={sheetDismiss} />
      </ParentDismissLayer>,
    );

    fireEvent.mouseDown(screen.getByTestId("action-sheet"));

    expect(sheetDismiss).toHaveBeenCalledTimes(1);
    expect(parentDismiss).not.toHaveBeenCalled();
  });

  it("renders action icons and group dividers for menu-style sheets", () => {
    render(
      <ActionSheet
        items={[
          {
            icon: <span data-testid="sheet-action-icon" />,
            label: "View racks",
            showGroupDivider: true,
            testId: "sheet-view-racks",
          },
          { label: "Edit site", testId: "sheet-edit-site" },
        ]}
        onClose={vi.fn()}
      />,
    );

    expect(screen.getByTestId("sheet-action-icon")).toBeInTheDocument();
    expect(screen.getByTestId("sheet-view-racks")).toHaveTextContent("View racks");
    expect(screen.getByTestId("sheet-view-racks").parentElement?.nextElementSibling).toHaveClass("border-border-10");
  });
});
