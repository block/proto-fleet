import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ModuleStatus } from "./ModuleStatus";

describe("ModuleStatus", () => {
  it.each([
    ["active", "Hashing", "normal"],
    ["offline", "Offline", "inactive"],
    ["inactive", "Sleeping", "sleeping"],
    ["error", "Needs attention", "error"],
    ["maintenance", "Sleeping", "sleeping"],
    ["updating", "Updating firmware", "pending"],
    ["needs_mining_pool", "Needs attention", "error"],
    ["reboot_required", "Needs attention", "error"],
    ["unknown", "Unknown", "inactive"],
    ["", "Unknown", "inactive"],
    ["future-controller-state", "Unknown", "inactive"],
  ])("renders %s with the shared status indicator", (state, label, indicator) => {
    render(<ModuleStatus state={state} />);
    expect(screen.getByText(label)).toBeInTheDocument();
    expect(screen.getByTestId("module-status-indicator")).toHaveAttribute("data-status", indicator);
    expect(screen.queryByTitle(/isolated/i)).not.toBeInTheDocument();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });
});
