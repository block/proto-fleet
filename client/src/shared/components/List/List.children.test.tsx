import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import List from "./List";
import Checkbox from "@/shared/components/Checkbox";

describe("List child rows", () => {
  it("renders parent-managed child selection and a mixed parent without adding children to bulk selection", () => {
    const selectBoard = vi.fn();
    const selectParents = vi.fn();
    render(
      <List<{ id: string; name: string }, string, "name">
        items={[{ id: "module", name: "Module" }]}
        itemKey="id"
        activeCols={["name"]}
        colTitles={{ name: "Name" }}
        colConfig={{ name: { width: "w-40", component: (item) => item.name } }}
        itemSelectable
        pageScopedSelection
        customSelectedItems={[]}
        customSetSelectedItems={selectParents}
        isRowPartiallySelected={() => true}
        getChildRows={() => [
          {
            key: "board",
            label: "Hashboard 1",
            cells: { name: "Hashboard 1" },
            selection: <Checkbox checked ariaLabel="Select Hashboard 1" onChange={selectBoard} />,
          },
        ]}
      />,
    );
    const child = screen.getByRole("row", { name: "Hashboard 1" });
    expect(screen.getByRole("checkbox", { name: "Select module" })).toHaveAttribute("aria-checked", "mixed");
    fireEvent.click(within(child).getByRole("checkbox"));
    expect(selectBoard).toHaveBeenCalledOnce();
    expect(selectParents).not.toHaveBeenCalled();
    fireEvent.click(within(screen.getByTestId("list-header")).getByRole("checkbox"));
    expect(selectParents).toHaveBeenCalledWith(["module"]);
  });
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
