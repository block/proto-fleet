import { MemoryRouter } from "react-router-dom";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import TicketDetailModal from "./TicketDetailModal";

describe("TicketDetailModal", () => {
  it("pins the ticket number in the title bar, constrains the body to scroll, and uses shared dropdown icons", () => {
    render(
      <MemoryRouter>
        <TicketDetailModal ticketId="1" ticketIds={["1", "2"]} onDismiss={vi.fn()} />
      </MemoryRouter>,
    );

    const modal = screen.getByTestId("modal");
    const titleBar = screen.getByRole("button", { name: "Close dialog" }).closest(".sticky");

    expect(modal).toHaveClass("!h-[min(720px,calc(100dvh-(--spacing(32))))]");
    expect(titleBar).toHaveTextContent("TK-0001");
    expect(modal).toHaveTextContent("TK-0001");
    expect(screen.getAllByText("TK-0001")).toHaveLength(1);

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
