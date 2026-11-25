import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import DurationSelector from "./DurationSelector";

const TEST_DURATIONS = ["1h", "24h", "5d"] as const;

describe("DurationSelector", () => {
  it("updates its selection when uncontrolled", async () => {
    const user = userEvent.setup();
    const onSelect = vi.fn();
    render(<DurationSelector durations={TEST_DURATIONS} onSelect={onSelect} />);

    expect(screen.getByRole("group", { name: "Time range" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "1h" })).toHaveAttribute("aria-pressed", "true");

    await user.click(screen.getByRole("button", { name: "24h" }));

    expect(onSelect).toHaveBeenCalledWith("24h");
    expect(screen.getByRole("button", { name: "1h" })).toHaveAttribute("aria-pressed", "false");
    expect(screen.getByRole("button", { name: "24h" })).toHaveAttribute("aria-pressed", "true");
  });

  it("uses a configurable group label", () => {
    render(<DurationSelector ariaLabel="Site resources" durations={TEST_DURATIONS} />);

    expect(screen.getByRole("group", { name: "Site resources" })).toBeInTheDocument();
    expect(screen.queryByRole("group", { name: "Time range" })).not.toBeInTheDocument();
  });

  it("follows controlled duration changes without changing locally", async () => {
    const user = userEvent.setup();
    const onSelect = vi.fn();
    const { rerender } = render(<DurationSelector duration="1h" durations={TEST_DURATIONS} onSelect={onSelect} />);

    await user.click(screen.getByRole("button", { name: "5d" }));

    expect(onSelect).toHaveBeenCalledWith("5d");
    expect(screen.getByRole("button", { name: "1h" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "5d" })).toHaveAttribute("aria-pressed", "false");

    rerender(<DurationSelector duration="24h" durations={TEST_DURATIONS} onSelect={onSelect} />);

    expect(screen.getByRole("button", { name: "1h" })).toHaveAttribute("aria-pressed", "false");
    expect(screen.getByRole("button", { name: "24h" })).toHaveAttribute("aria-pressed", "true");
  });
});
