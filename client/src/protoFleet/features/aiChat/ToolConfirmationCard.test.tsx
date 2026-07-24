import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";

import ToolConfirmationCard from "./ToolConfirmationCard";
import type { ToolConfirmation } from "./types";

const pendingConfirmation: ToolConfirmation = {
  id: "confirmation-1",
  toolCallId: "call-1",
  title: "Move 2 miners?",
  description: "Minerbot will move these miners.",
  confirmLabel: "Move miners",
  details: [
    { label: "Destination rack ID", value: "21" },
    { label: "Miners", value: "miner-a, miner-b" },
  ],
  status: "pending",
  sequence: 2,
};

describe("ToolConfirmationCard", () => {
  test("shows exact action details and exposes approve and cancel decisions", () => {
    const onResolve = vi.fn();
    render(<ToolConfirmationCard confirmation={pendingConfirmation} onResolve={onResolve} />);

    expect(screen.getByRole("heading", { name: "Move 2 miners?" })).toBeInTheDocument();
    expect(screen.getByText("miner-a, miner-b")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onResolve).toHaveBeenCalledWith(pendingConfirmation, "cancel");

    fireEvent.click(screen.getByRole("button", { name: "Move miners" }));
    expect(onResolve).toHaveBeenCalledWith(pendingConfirmation, "approve");
  });

  test("locks both decisions while the response is being submitted", () => {
    render(
      <ToolConfirmationCard
        confirmation={{ ...pendingConfirmation, status: "submitting", decision: "approve" }}
        onResolve={vi.fn()}
      />,
    );

    expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Submitting" })).toBeDisabled();
  });
});
