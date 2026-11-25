import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import RadialGauge from "./RadialGauge";

describe("RadialGauge", () => {
  it("renders compact center text at the gauge midpoint", () => {
    render(<RadialGauge value={80} size={40} centerText="80%" centerTextFontSize={12} />);

    const centerText = screen.getByText("80%");
    expect(centerText).toHaveAttribute("x", "20");
    expect(centerText).toHaveAttribute("y", "20");
    expect(centerText).toHaveAttribute("text-anchor", "middle");
    expect(centerText).toHaveAttribute("dominant-baseline", "central");
  });
});
