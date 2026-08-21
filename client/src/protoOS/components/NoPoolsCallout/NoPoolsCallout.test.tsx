import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, type Mock, test, vi } from "vitest";

import NoPoolsCallout from "./NoPoolsCallout";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useNavigate } from "@/shared/hooks/useNavigate";

const mockNavigate = vi.fn();

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: vi.fn(),
}));

vi.mock("@/shared/hooks/useNavigate", () => ({
  useNavigate: vi.fn(() => mockNavigate),
}));

describe("NoPoolsCallout", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    (useNavigate as Mock).mockReturnValue(mockNavigate);
  });

  test("direct mode links to the absolute pools settings path", () => {
    (useMinerHosting as Mock).mockReturnValue({ minerRoot: "", isFleetHosted: false });

    render(<NoPoolsCallout arePoolsConfigured={false} />);
    fireEvent.click(screen.getByText("Add mining pools"));

    expect(mockNavigate).toHaveBeenCalledWith("/settings/mining-pools");
  });

  test("fleet-hosted mode stays in the embed and frames the CTA as view-only", () => {
    (useMinerHosting as Mock).mockReturnValue({ minerRoot: "/miners/miner-1", isFleetHosted: true });

    render(<NoPoolsCallout arePoolsConfigured={false} />);

    // Adding isn't possible from the read-only embedded view, so the CTA is "view".
    fireEvent.click(screen.getByText("View pool settings"));

    expect(mockNavigate).toHaveBeenCalledWith("/miners/miner-1/settings/mining-pools");
  });
});
