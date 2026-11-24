import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import FanCard from "./FanCard";
import TankModuleGrid from "./TankModuleGrid";

describe("unavailable observations", () => {
  it("does not treat missing module observations as healthy", () => {
    render(<TankModuleGrid cols={2} rows={1} modules={["healthy"]} label="Tank 1" />);
    expect(screen.getAllByTestId("tank-module")[1]).toHaveAttribute("data-module-state", "unknown");
  });
  it("shows an unavailable fan reading without an unsupported switch", () => {
    render(<FanCard label="Fan 1" on={undefined} speedPercent={undefined} powerLabel="—" />);
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
    expect(screen.queryByText("0 W")).not.toBeInTheDocument();
    expect(screen.getByText("— W")).toBeInTheDocument();
  });
});
