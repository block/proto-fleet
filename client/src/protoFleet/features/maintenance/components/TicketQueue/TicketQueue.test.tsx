import { MemoryRouter } from "react-router-dom";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import TicketQueue from "./TicketQueue";

describe("TicketQueue", () => {
  it("renders every active status lane when opened in board view", () => {
    render(
      <MemoryRouter>
        <TicketQueue initialViewMode="kanban" />
      </MemoryRouter>,
    );

    expect(screen.getByText(/^Open \(/)).toBeInTheDocument();
    expect(screen.getByText(/^In Progress \(/)).toBeInTheDocument();
    expect(screen.getByText(/^On Hold \(/)).toBeInTheDocument();
    expect(screen.getByText("Sent to Vendor (1)")).toBeInTheDocument();
  });
});
