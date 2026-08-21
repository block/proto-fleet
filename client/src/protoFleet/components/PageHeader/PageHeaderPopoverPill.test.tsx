import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import PageHeaderPopoverPill from "./PageHeaderPopoverPill";

function renderPageHeaderPopoverPill() {
  return render(
    <PageHeaderPopoverPill
      ariaLabel="Toggle details"
      dotClassName="bg-core-accent-fill"
      triggerClassName="first-trigger second-trigger"
      triggerLabel="Details"
    >
      {() => <div>Popover content</div>}
    </PageHeaderPopoverPill>,
  );
}

describe("PageHeaderPopoverPill", () => {
  it("keeps trigger clicks ignored when the trigger has multiple classes", () => {
    renderPageHeaderPopoverPill();

    const trigger = screen.getByRole("button", { name: "Toggle details" });
    fireEvent.click(trigger);

    expect(screen.getByText("Popover content")).toBeInTheDocument();

    fireEvent.mouseDown(trigger);
    fireEvent.click(trigger);

    expect(screen.queryByText("Popover content")).not.toBeInTheDocument();
  });

  it("allows the trigger label to truncate inside constrained header space", () => {
    renderPageHeaderPopoverPill();

    const trigger = screen.getByRole("button", { name: "Toggle details" });
    const triggerWrapper = trigger.closest(".first-trigger");

    expect(triggerWrapper).toHaveClass("min-w-0");
    expect(trigger).toHaveClass("min-w-0", "max-w-full");
    expect(screen.getByText("Details")).toHaveClass("min-w-0", "truncate");
  });
});
