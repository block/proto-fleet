import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import List from "./List";

describe("List child rows", () => {
  it("keeps children outside selection and the parent count", () => {
    const click = vi.fn();
    render(
      <List<{ id: string; name: string }, string, "name">
        items={[{ id: "module", name: "Module" }]}
        itemKey="id"
        activeCols={["name"]}
        colTitles={{ name: "Name" }}
        colConfig={{ name: { width: "w-40", component: (item) => item.name } }}
        itemSelectable
        total={1}
        renderActionBar={(_selected, _clear, _mode, totalSelectable) => (
          <div data-testid="total-selectable">{totalSelectable}</div>
        )}
        onRowClick={click}
        getChildRows={() => [{ key: "board", label: "Hashboard 1", cells: { name: "Hashboard 1" } }]}
      />,
    );
    const child = screen.getByRole("row", { name: "Hashboard 1" });
    expect(within(child).queryByRole("checkbox")).not.toBeInTheDocument();
    expect(child).not.toHaveAttribute("tabindex");
    expect(screen.getAllByTestId("list-row")).toHaveLength(1);
    expect(screen.getAllByRole("checkbox")).toHaveLength(2);
    fireEvent.click(child);
    expect(click).not.toHaveBeenCalled();
    fireEvent.click(screen.getByTestId("list-row"));
    expect(click).toHaveBeenCalledTimes(1);
    fireEvent.click(within(screen.getByTestId("list-header")).getByRole("checkbox"));
    expect(screen.getByTestId("total-selectable")).toHaveTextContent("1");
  });
});
