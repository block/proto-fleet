import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import MinerIssuesCell from "./MinerIssuesCell";

vi.mock("./MinerIssues", () => ({
  default: ({ onClick }: { onClick: () => void }) => (
    <button onClick={onClick} data-testid="miner-issues">
      Issues
    </button>
  ),
}));

describe("MinerIssuesCell", () => {
  it("calls onOpenStatusFlow when issues are clicked", async () => {
    const user = userEvent.setup();
    const onOpenStatusFlow = vi.fn();

    render(<MinerIssuesCell deviceIdentifier="test-device-id" onOpenStatusFlow={onOpenStatusFlow} />);

    await user.click(screen.getByTestId("miner-issues"));

    expect(onOpenStatusFlow).toHaveBeenCalledTimes(1);
    expect(onOpenStatusFlow).toHaveBeenCalledWith("test-device-id");
  });
});
