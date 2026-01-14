import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import MinerType from "./MinerType";
import { useMinerModel } from "@/protoFleet/store";

vi.mock("@/protoFleet/store", () => ({
  useMinerModel: vi.fn(),
}));

const mockUseMinerModel = vi.mocked(useMinerModel);

describe("MinerType", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders the model name when available", () => {
    mockUseMinerModel.mockReturnValue("Proto Rig");

    render(<MinerType deviceIdentifier="test-device-1" />);

    expect(screen.getByText("Proto Rig")).toBeInTheDocument();
  });

  it("renders placeholder when model is null", () => {
    mockUseMinerModel.mockReturnValue(null as any);

    render(<MinerType deviceIdentifier="test-device-2" />);

    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("renders placeholder when model is undefined", () => {
    mockUseMinerModel.mockReturnValue(undefined as any);

    render(<MinerType deviceIdentifier="test-device-3" />);

    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("renders placeholder when model is empty string", () => {
    mockUseMinerModel.mockReturnValue("");

    render(<MinerType deviceIdentifier="test-device-4" />);

    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("renders Bitmain model names", () => {
    mockUseMinerModel.mockReturnValue("Antminer S19");

    render(<MinerType deviceIdentifier="test-device-5" />);

    expect(screen.getByText("Antminer S19")).toBeInTheDocument();
  });

  it("calls useMinerModel with correct deviceIdentifier", () => {
    mockUseMinerModel.mockReturnValue("Proto Rig");

    render(<MinerType deviceIdentifier="specific-miner-id" />);

    expect(mockUseMinerModel).toHaveBeenCalledWith("specific-miner-id");
  });
});
