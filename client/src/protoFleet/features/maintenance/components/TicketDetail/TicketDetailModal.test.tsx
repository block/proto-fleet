import { MemoryRouter } from "react-router-dom";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import TicketDetailModal from "./TicketDetailModal";

describe("TicketDetailModal", () => {
  it("constrains the detail body to scroll and uses shared dropdown icons for its actions", () => {
    render(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" ticketIds={["1", "2"]} onDismiss={vi.fn()} />
      </MemoryRouter>,
    );

    expect(screen.getByTestId("modal")).toHaveClass("!h-[min(720px,calc(100dvh-(--spacing(32))))]");

    const assignMenuButton = screen
      .getAllByRole("button", { name: "Assign" })
      .find((button) => button.getAttribute("aria-haspopup") === "menu");
    const statusMenuButton = screen.getByRole("button", { name: "Update status" });

    expect(assignMenuButton).toBeDefined();
    expect(assignMenuButton?.querySelector("svg")).not.toBeNull();
    expect(assignMenuButton).not.toHaveTextContent("▾");
    expect(statusMenuButton).toHaveAttribute("aria-haspopup", "menu");
    expect(statusMenuButton.querySelector("svg")).not.toBeNull();
  });
});
