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
});
