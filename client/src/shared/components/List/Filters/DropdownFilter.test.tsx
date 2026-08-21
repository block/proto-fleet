import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import DropdownFilter from "./DropdownFilter";

describe("DropdownFilter", () => {
  it("closes after selection when closeOnSelect is enabled", async () => {
    const handleSelect = vi.fn();

    render(
      <DropdownFilter
        title="Sort"
        options={[
          { id: "name", label: "Name" },
          { id: "status", label: "Status" },
        ]}
        selectedOptions={["name"]}
        onSelect={handleSelect}
        showSelectAll={false}
        closeOnSelect
      />,
    );

    fireEvent.click(screen.getByTestId("filter-dropdown-Sort"));

    await waitFor(() => {
      expect(screen.getByTestId("dropdown-filter-popover")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId("filter-option-status"));

    expect(handleSelect).toHaveBeenCalledWith(["name", "status"]);

    await waitFor(() => {
      expect(screen.queryByTestId("dropdown-filter-popover")).not.toBeInTheDocument();
    });
  });
});
