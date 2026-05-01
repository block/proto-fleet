import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import TabStrip, { TabStripItem } from "./TabStrip";

describe("TabStrip", () => {
  it("renders items as tabs and marks the active one with aria-selected", () => {
    render(
      <TabStrip activeId="b" onSelect={() => {}} ariaLabel="Demo">
        <TabStripItem id="a" label="A" testId="tab-a" />
        <TabStripItem id="b" label="B" testId="tab-b" />
      </TabStrip>,
    );

    const tabA = screen.getByTestId("tab-a-activate");
    const tabB = screen.getByTestId("tab-b-activate");

    expect(tabA).toHaveAttribute("aria-selected", "false");
    expect(tabB).toHaveAttribute("aria-selected", "true");
  });

  it("invokes onSelect with the item id when clicked", async () => {
    const onSelect = vi.fn();
    const user = userEvent.setup();

    render(
      <TabStrip activeId="a" onSelect={onSelect}>
        <TabStripItem id="a" label="A" testId="tab-a" />
        <TabStripItem id="b" label="B" testId="tab-b" />
      </TabStrip>,
    );

    await user.click(screen.getByTestId("tab-b-activate"));
    expect(onSelect).toHaveBeenCalledWith("b");
  });

  it("does not call onSelect when the tab is disabled", async () => {
    const onSelect = vi.fn();
    const user = userEvent.setup();

    render(
      <TabStrip activeId="a" onSelect={onSelect}>
        <TabStripItem id="a" label="A" testId="tab-a" />
        <TabStripItem id="b" label="B" disabled testId="tab-b" />
      </TabStrip>,
    );

    await user.click(screen.getByTestId("tab-b-activate"));
    expect(onSelect).not.toHaveBeenCalled();
  });

  it("renders leading and trailing adornments around the activate button", () => {
    render(
      <TabStrip activeId="a" onSelect={() => {}}>
        <TabStripItem
          id="a"
          label="A"
          testId="tab-a"
          leading={<span data-testid="lead">L</span>}
          trailing={<span data-testid="trail">T</span>}
        />
      </TabStrip>,
    );

    expect(screen.getByTestId("lead")).toBeInTheDocument();
    expect(screen.getByTestId("trail")).toBeInTheDocument();
  });

  it("throws if TabStripItem is rendered outside TabStrip", () => {
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
    expect(() => render(<TabStripItem id="a" label="A" />)).toThrow(/inside <TabStrip>/);
    consoleError.mockRestore();
  });
});
