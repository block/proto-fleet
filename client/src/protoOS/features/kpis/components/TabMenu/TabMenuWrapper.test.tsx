import { useLocation, useNavigate } from "react-router-dom";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, type Mock, vi } from "vitest";

import TabMenuWrapper from "./TabMenuWrapper";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useMiner, useTemperatureUnit } from "@/protoOS/store";

const mockNavigate = vi.fn();

vi.mock("react-router-dom", async () => {
  const actual = await vi.importActual("react-router-dom");
  return {
    ...actual,
    useNavigate: vi.fn(),
    useLocation: vi.fn(() => ({ pathname: "/", search: "", hash: "", state: null, key: "default" })),
  };
});

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: vi.fn(),
}));

vi.mock("@/protoOS/store", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/protoOS/store")>()),
  useMiner: vi.fn(() => undefined),
  useTemperatureUnit: vi.fn(() => "F"),
}));

describe("TabMenuWrapper", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    (useLocation as Mock).mockReturnValue({ pathname: "/", search: "", hash: "", state: null, key: "default" });
    (useNavigate as Mock).mockReturnValue(mockNavigate);
  });

  it("navigates to absolute KPI paths in standalone protoOS (empty minerRoot)", () => {
    (useMinerHosting as Mock).mockReturnValue({ minerRoot: "" });

    render(<TabMenuWrapper />);
    fireEvent.click(screen.getByText("Hashrate").closest("button") as HTMLButtonElement);

    expect(mockNavigate).toHaveBeenCalledWith("/hashrate");
  });

  it("prefixes minerRoot so KPI tabs stay inside the embedded fleet miner view", () => {
    (useMinerHosting as Mock).mockReturnValue({ minerRoot: "/miners/miner-1" });

    render(<TabMenuWrapper />);
    fireEvent.click(screen.getByText("Efficiency").closest("button") as HTMLButtonElement);

    // Without the minerRoot prefix this would navigate to "/efficiency", which
    // ProtoFleet's "/:siteScope" route treats as an unknown site and redirects
    // back to the dashboard.
    expect(mockNavigate).toHaveBeenCalledWith("/miners/miner-1/efficiency");
  });

  it("renders the temperature tab in the preferred unit with a single degree glyph", () => {
    (useMinerHosting as Mock).mockReturnValue({ minerRoot: "" });
    (useTemperatureUnit as Mock).mockReturnValue("F");
    (useMiner as Mock).mockReturnValue({ temperature: { latest: { value: 62, units: "°C" } } });

    render(<TabMenuWrapper />);

    expect(screen.getByText("143.6")).toBeInTheDocument();
    expect(screen.getByText("°F")).toBeInTheDocument();
    expect(screen.queryByText("°°F")).toBeNull();
  });
});
