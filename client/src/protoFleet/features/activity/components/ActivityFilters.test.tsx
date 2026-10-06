import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import ActivityFilters from "./ActivityFilters";
import { EventTypeOptionSchema, UserOptionSchema } from "@/protoFleet/api/generated/activity/v1/activity_pb";

const defaultProps = {
  searchValue: "",
  onSearchChange: vi.fn(),
  eventTypes: [
    create(EventTypeOptionSchema, {
      eventType: "login",
      eventCategory: "auth",
    }),
  ],
  scopeTypes: ["rack"],
  users: [
    create(UserOptionSchema, {
      userId: "user-1",
      username: "alice",
    }),
  ],
  selectedTypes: [],
  selectedScopes: [],
  selectedUsers: [],
  onTypesChange: vi.fn(),
  onScopesChange: vi.fn(),
  onUsersChange: vi.fn(),
};

describe("ActivityFilters", () => {
  it("spans search across the toolbar and aligns actions with filter controls", () => {
    render(<ActivityFilters {...defaultProps} actions={<button type="button">Export CSV</button>} />);

    expect(screen.getByTestId("activity-search-row")).toHaveClass("w-full", "min-w-0");
    expect(screen.getByTestId("activity-toolbar-row")).toHaveClass("justify-between");

    const exportButton = screen.getByRole("button", { name: "Export CSV" });
    expect(exportButton).toBeInTheDocument();
    expect(exportButton.parentElement).toHaveClass("ml-auto", "shrink-0");
  });

  it("uses live fleet-style checkbox filters without an apply action", async () => {
    const onTypesChange = vi.fn();
    render(<ActivityFilters {...defaultProps} onTypesChange={onTypesChange} />);

    expect(screen.getByLabelText("Search activity")).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("filter-nested-add-filter"));
    expect(screen.queryByText("Apply")).not.toBeInTheDocument();
    expect(screen.queryByText("Clear all")).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId("nested-dropdown-filter-row-type"));

    await waitFor(() => {
      expect(screen.getByTestId("filter-option-login")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId("filter-option-login"));

    expect(onTypesChange).toHaveBeenCalledWith(["login"]);
  });

  it("shows clear affordances once selections exist", () => {
    const onTypesChange = vi.fn();
    const onScopesChange = vi.fn();
    const onUsersChange = vi.fn();

    render(
      <ActivityFilters
        {...defaultProps}
        selectedTypes={["login"]}
        onTypesChange={onTypesChange}
        onScopesChange={onScopesChange}
        onUsersChange={onUsersChange}
      />,
    );

    expect(screen.getByTestId("active-filter-type")).toHaveTextContent("Log in");

    fireEvent.click(screen.getByTestId("filter-nested-add-filter"));
    fireEvent.click(screen.getByText("Clear all"));

    expect(onTypesChange).toHaveBeenCalledWith([]);
    expect(onScopesChange).toHaveBeenCalledWith([]);
    expect(onUsersChange).toHaveBeenCalledWith([]);
  });

  it("normalizes and deduplicates activity type filter options while preserving raw event types", async () => {
    const onTypesChange = vi.fn();

    render(
      <ActivityFilters
        {...defaultProps}
        eventTypes={[
          create(EventTypeOptionSchema, {
            eventType: "login",
            eventCategory: "auth",
          }),
          create(EventTypeOptionSchema, {
            eventType: "set_power_target",
            eventCategory: "device_command",
          }),
          create(EventTypeOptionSchema, {
            eventType: "set_power_target.completed",
            eventCategory: "device_command",
          }),
          create(EventTypeOptionSchema, {
            eventType: "set_rack_slot",
            eventCategory: "fleet_management",
          }),
          create(EventTypeOptionSchema, {
            eventType: "clear_rack_slot",
            eventCategory: "fleet_management",
          }),
        ]}
        onTypesChange={onTypesChange}
      />,
    );

    fireEvent.click(screen.getByTestId("filter-nested-add-filter"));
    fireEvent.click(screen.getByTestId("nested-dropdown-filter-row-type"));

    await waitFor(() => {
      expect(screen.getByTestId("filter-option-set_power_target")).toBeInTheDocument();
    });

    expect(screen.getAllByText("Update power target")).toHaveLength(1);
    expect(screen.getAllByText("Update rack position")).toHaveLength(1);

    const loginOption = screen.getByTestId("filter-option-login");
    expect(loginOption.parentElement?.nextElementSibling).toHaveClass("border-border-10");

    fireEvent.click(screen.getByTestId("filter-option-set_power_target"));

    expect(onTypesChange).toHaveBeenCalledWith(["set_power_target", "set_power_target.completed"]);
  });
});
