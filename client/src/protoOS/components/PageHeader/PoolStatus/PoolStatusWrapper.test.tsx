import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, type Mock, test, vi } from "vitest";

import PoolStatusWrapper from "./PoolStatusWrapper";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useNavigate } from "@/shared/hooks/useNavigate";

const mockNavigate = vi.fn();

// Stub the presentational status chip down to a button that fires the view-pools handler.
vi.mock("./PoolStatus", () => ({
  default: ({ onClickViewPools }: { onClickViewPools: () => void }) => (
    <button data-testid="view-pools" onClick={onClickViewPools} />
  ),
}));

vi.mock("@/protoOS/store", () => ({
  usePoolsInfo: vi.fn(() => undefined),
}));

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: vi.fn(),
}));

vi.mock("@/shared/hooks/useNavigate", () => ({
  useNavigate: vi.fn(() => mockNavigate),
}));

describe("PoolStatusWrapper view-pools navigation", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    (useNavigate as Mock).mockReturnValue(mockNavigate);
  });

  test("direct mode navigates to the absolute pools settings path", () => {
    (useMinerHosting as Mock).mockReturnValue({ minerRoot: "" });

    render(<PoolStatusWrapper />);
    fireEvent.click(screen.getByTestId("view-pools"));

    expect(mockNavigate).toHaveBeenCalledWith("/settings/mining-pools");
  });

  test("fleet-hosted mode stays inside the embedded miner view", () => {
    (useMinerHosting as Mock).mockReturnValue({ minerRoot: "/miners/miner-1" });

    render(<PoolStatusWrapper />);
    fireEvent.click(screen.getByTestId("view-pools"));

    expect(mockNavigate).toHaveBeenCalledWith("/miners/miner-1/settings/mining-pools");
  });
});
