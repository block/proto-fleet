import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import MinerStatusCell from "./MinerStatusCell";

vi.mock("./MinerStatus", () => ({
  default: ({ onClick }: { onClick: () => void }) => (
    <button onClick={onClick} data-testid="miner-status">
      Status
    </button>
  ),
}));

describe("MinerStatusCell", () => {
  it("calls onOpenStatusFlow when status is clicked", async () => {
    const user = userEvent.setup();
    const onOpenStatusFlow = vi.fn();

    render(<MinerStatusCell deviceIdentifier="test-device-id" onOpenStatusFlow={onOpenStatusFlow} />);

    await user.click(screen.getByTestId("miner-status"));

    expect(onOpenStatusFlow).toHaveBeenCalledTimes(1);
    expect(onOpenStatusFlow).toHaveBeenCalledWith("test-device-id");
  });
});
