import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import CreateTicketModal from "./CreateTicketModal";
import { DEFAULT_ACTIVE_SITE } from "@/protoFleet/store/types/activeSite";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

const renderModal = () => render(<CreateTicketModal onDismiss={vi.fn()} onSuccess={vi.fn()} />);

describe("CreateTicketModal", () => {
  beforeEach(() => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = DEFAULT_ACTIVE_SITE;
    });
  });

  it("defaults to a miner ticket and a selected site without duplicating the description field", () => {
    renderModal();

    expect(screen.getByRole("button", { name: "Category" })).toHaveTextContent("Miner");
    expect(screen.getByRole("button", { name: "Site" })).toHaveTextContent("Denver");
    expect(screen.getByRole("textbox", { name: "Issue description" })).toBeInTheDocument();
    expect(screen.queryByRole("textbox", { name: /Notes/ })).not.toBeInTheDocument();
  });

  it("uses the current site and exposes category as a dropdown", async () => {
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "site", id: "8", slug: "austin" };
    });
    const user = userEvent.setup();
    renderModal();

    expect(screen.getByRole("button", { name: "Site" })).toHaveTextContent("Austin");

    const category = screen.getByRole("button", { name: "Category" });
    expect(category).toHaveAttribute("aria-haspopup", "listbox");

    await user.click(category);

    expect(category).toHaveAttribute("aria-expanded", "true");
  });
});
