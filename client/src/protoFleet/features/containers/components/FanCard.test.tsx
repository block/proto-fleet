import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import FanCard from "./FanCard";

describe("FanCard", () => {
  it.each([
    [true, false],
    [false, true],
  ])("reports the next power state when on is %s", async (on, expected) => {
    const user = userEvent.setup();
    const onToggle = vi.fn();
    render(<FanCard label="Fan 1" speedPercent={50} speedLabel="1,000" on={on} onToggle={onToggle} />);

    await user.click(screen.getByRole("checkbox", { name: "Fan 1 power" }));

    expect(onToggle).toHaveBeenCalledWith(expected);
  });
});
