import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ModuleStatus } from "./ModuleStatus";

describe("ModuleStatus", () => {
  it.each([
    ["hashing", "Hashing", "normal"],
    ["offline", "Offline", "inactive"],
    ["sleeping", "Sleeping", "sleeping"],
    ["attention", "Needs attention", "error"],
    ["isolated", "Needs attention", "error"],
    ["unknown", "Unknown", "inactive"],
    ["", "Unknown", "inactive"],
    ["future-controller-state", "Unknown", "inactive"],
  ])("renders %s with the shared status indicator", (state, label, indicator) => {
    render(<ModuleStatus state={state} />);
    expect(screen.getByText(label)).toBeInTheDocument();
    expect(screen.getByTestId("module-status-indicator")).toHaveAttribute("data-status", indicator);
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });
});
