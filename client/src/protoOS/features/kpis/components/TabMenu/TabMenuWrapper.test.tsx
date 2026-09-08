import { useLocation, useNavigate, useParams } from "react-router-dom";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, type Mock, vi } from "vitest";

import TabMenuWrapper from "./TabMenuWrapper";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

const mockNavigate = vi.fn();

vi.mock("react-router-dom", async () => {
  const actual = await vi.importActual("react-router-dom");
  return {
    ...actual,
    useNavigate: vi.fn(),
    useLocation: vi.fn(() => ({ pathname: "/", search: "", hash: "", state: null, key: "default" })),
    useParams: vi.fn(() => ({})),
  };
});

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: vi.fn(),
}));

vi.mock("@/protoOS/store", () => ({
  useMiner: vi.fn(() => undefined),
  useTemperatureUnit: vi.fn(() => "F"),
  convertAndFormatMeasurement: vi.fn(() => "--"),
  convertValueUnits: vi.fn(() => ({ value: null })),
}));

describe("TabMenuWrapper", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    (useLocation as Mock).mockReturnValue({ pathname: "/", search: "", hash: "", state: null, key: "default" });
    (useNavigate as Mock).mockReturnValue(mockNavigate);
    (useParams as Mock).mockReturnValue({});
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

  it("preserves the selected module route when switching KPI tabs", () => {
    (useMinerHosting as Mock).mockReturnValue({ minerRoot: "" });
    (useParams as Mock).mockReturnValue({ tankId: "1", moduleId: "2" });

    render(<TabMenuWrapper />);
    fireEvent.click(screen.getByText("Power Usage").closest("button") as HTMLButtonElement);

    expect(mockNavigate).toHaveBeenCalledWith("/tanks/1/modules/2/power-usage");
  });

  it("marks hashrate active on the selected module index route", () => {
    (useMinerHosting as Mock).mockReturnValue({ minerRoot: "" });
    (useLocation as Mock).mockReturnValue({
      pathname: "/tanks/1/modules/2",
      search: "",
      hash: "",
      state: null,
      key: "default",
    });
    (useParams as Mock).mockReturnValue({ tankId: "1", moduleId: "2" });

    render(<TabMenuWrapper />);

    expect(screen.getByRole("tab", { name: /Hashrate/ })).toHaveAttribute("aria-selected", "true");
  });

  it("encodes module route params when building scoped KPI tab paths", () => {
    (useMinerHosting as Mock).mockReturnValue({ minerRoot: "" });
    (useParams as Mock).mockReturnValue({ tankId: "container:ct1:tank:1", moduleId: "module:1" });

    render(<TabMenuWrapper />);
    fireEvent.click(screen.getByText("Temperature").closest("button") as HTMLButtonElement);

    expect(mockNavigate).toHaveBeenCalledWith("/tanks/container%3Act1%3Atank%3A1/modules/module%3A1/temperature");
  });
});
