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
    (useMinerHosting as Mock).mockReturnValue({ mode: "direct", isFleetHosted: false });

    renderExpanded();

    expect(screen.getByText("Authentication")).toBeInTheDocument();
    expect(screen.getByText("Pools")).toBeInTheDocument();
    expect(screen.getByText("General")).toBeInTheDocument();
  });

  test("fleet-hosted mode hides Authentication but keeps Pools (read-only view)", () => {
    (useMinerHosting as Mock).mockReturnValue({ mode: "fleet", isFleetHosted: true });

    renderExpanded();

    // Authentication has no useful read-only view in fleet mode.
    expect(screen.queryByText("Authentication")).not.toBeInTheDocument();
    // Pools stays visible so operators can see the miner's current pools.
    expect(screen.getByText("Pools")).toBeInTheDocument();
    expect(screen.getByText("General")).toBeInTheDocument();
    expect(screen.getByText("Hardware")).toBeInTheDocument();
    expect(screen.getByText("Cooling")).toBeInTheDocument();
  });
});
