import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import MinerModel from "./MinerModel";
import { useMinerModel } from "@/protoFleet/store";

vi.mock("@/protoFleet/store", () => ({
  useMinerModel: vi.fn(),
}));

const mockUseMinerModel = vi.mocked(useMinerModel);

describe("MinerModel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders the model name when available", () => {
    mockUseMinerModel.mockReturnValue("Proto Rig");

    render(<MinerModel deviceIdentifier="test-device-1" />);

    expect(screen.getByText("Proto Rig")).toBeInTheDocument();
  });

  it("renders placeholder when model is null", () => {
    mockUseMinerModel.mockReturnValue(null as any);

    render(<MinerModel deviceIdentifier="test-device-2" />);

    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("renders placeholder when model is undefined", () => {
    mockUseMinerModel.mockReturnValue(undefined as any);

    render(<MinerModel deviceIdentifier="test-device-3" />);

    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("renders placeholder when model is empty string", () => {
    mockUseMinerModel.mockReturnValue("");

    render(<MinerModel deviceIdentifier="test-device-4" />);

    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("renders Bitmain model names", () => {
    mockUseMinerModel.mockReturnValue("Antminer S19");

    render(<MinerModel deviceIdentifier="test-device-5" />);

    expect(screen.getByText("Antminer S19")).toBeInTheDocument();
  });

  it("calls useMinerModel with correct deviceIdentifier", () => {
    mockUseMinerModel.mockReturnValue("Proto Rig");

    render(<MinerModel deviceIdentifier="specific-miner-id" />);

    expect(mockUseMinerModel).toHaveBeenCalledWith("specific-miner-id");
  });
});
