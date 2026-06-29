import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, type Mock, test, vi } from "vitest";

import AppNavigationItems from "./AppNavigationItems";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: vi.fn(),
}));

describe("AppNavigationItems", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  const renderExpanded = () =>
    // pageName under "settings" opens the settings accordion on mount.
    render(<AppNavigationItems onClick={vi.fn()} pageName="settings/general" />);

  test("direct mode shows the Fleet-managed settings", () => {
    (useMinerHosting as Mock).mockReturnValue({ mode: "direct" });

    renderExpanded();

    expect(screen.getByText("Authentication")).toBeInTheDocument();
    expect(screen.getByText("Pools")).toBeInTheDocument();
    expect(screen.getByText("General")).toBeInTheDocument();
  });

  test("fleet-hosted mode hides Authentication and Pools (Fleet-managed)", () => {
    (useMinerHosting as Mock).mockReturnValue({ mode: "fleet" });

    renderExpanded();

    expect(screen.queryByText("Authentication")).not.toBeInTheDocument();
    expect(screen.queryByText("Pools")).not.toBeInTheDocument();
    // Other settings remain available.
    expect(screen.getByText("General")).toBeInTheDocument();
    expect(screen.getByText("Hardware")).toBeInTheDocument();
    expect(screen.getByText("Cooling")).toBeInTheDocument();
  });
});
