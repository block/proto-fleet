import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import OfflineMiners from ".";

describe("Offline Miners", () => {
  const chartSelector = '[data-testid="stat-chart"]';

  const successClass = "bg-intent-success-fill";
  const warningClass = "bg-core-accent-fill";
  const criticalClass = "bg-intent-critical-fill";

  it("renders correct stats when all miners are active", () => {
    const { getByText, getAllByText } = render(
      <OfflineMiners activeMiners={100} inactiveMiners={0} offlineMiners={0} />,
    );

    const activeText = getByText("100 active miners");
    expect(activeText).toBeInTheDocument();
    expect(getByText("100.0% of fleet")).toBeInTheDocument();
    const activeChart =
      activeText.parentElement?.parentElement?.querySelector(chartSelector);
    expect(activeChart).toBeInTheDocument();
    expect(activeChart).toHaveClass(successClass);

    expect(getAllByText("0.0% of fleet")).toHaveLength(2);

    const inactiveText = getByText("0 inactive miners");
    expect(inactiveText).toBeInTheDocument();
    const inactiveChart =
      inactiveText.parentElement?.parentElement?.querySelector(chartSelector);
    expect(inactiveChart).toBeInTheDocument();
    expect(inactiveChart).toHaveClass(successClass);

    const offlineText = getByText("0 offline miners");
    expect(offlineText).toBeInTheDocument();
    const offlineChart =
      offlineText.parentElement?.parentElement?.querySelector(chartSelector);
    expect(offlineChart).toBeInTheDocument();
    expect(offlineChart).toHaveClass(successClass);
  });

  it("renders correct stats when some miners are inactive or offline", () => {
    const { getByText } = render(
      <OfflineMiners activeMiners={96} inactiveMiners={3} offlineMiners={1} />,
    );

    const activeText = getByText("96 active miners");
    expect(activeText).toBeInTheDocument();
    expect(getByText("96.0% of fleet")).toBeInTheDocument();
    const activeChart =
      activeText.parentElement?.parentElement?.querySelector(chartSelector);
    expect(activeChart).toBeInTheDocument();
    expect(activeChart).toHaveClass(successClass);

    const inactiveText = getByText("3 inactive miners");
    expect(inactiveText).toBeInTheDocument();
    expect(getByText("3.0% of fleet")).toBeInTheDocument();
    const inactiveChart =
      inactiveText.parentElement?.parentElement?.querySelector(chartSelector);
    expect(inactiveChart).toBeInTheDocument();
    expect(inactiveChart).toHaveClass(warningClass);

    const offlineText = getByText("1 offline miners");
    expect(offlineText).toBeInTheDocument();
    expect(getByText("1.0% of fleet")).toBeInTheDocument();
    const offlineChart =
      offlineText.parentElement?.parentElement?.querySelector(chartSelector);
    expect(offlineChart).toBeInTheDocument();
    expect(offlineChart).toHaveClass(warningClass);
  });

  it("renders critical stats when there are too many inactive and offline miners", () => {
    const { getByText } = render(
      <OfflineMiners activeMiners={87} inactiveMiners={6} offlineMiners={7} />,
    );

    const activeText = getByText("87 active miners");
    expect(activeText).toBeInTheDocument();
    expect(getByText("87.0% of fleet")).toBeInTheDocument();
    const activeChart =
      activeText.parentElement?.parentElement?.querySelector(chartSelector);
    expect(activeChart).toBeInTheDocument();
    expect(activeChart).toHaveClass(criticalClass);

    const inactiveText = getByText("6 inactive miners");
    expect(inactiveText).toBeInTheDocument();
    expect(getByText("6.0% of fleet")).toBeInTheDocument();
    const inactiveChart =
      inactiveText.parentElement?.parentElement?.querySelector(chartSelector);
    expect(inactiveChart).toBeInTheDocument();
    expect(inactiveChart).toHaveClass(criticalClass);

    const offlineText = getByText("7 offline miners");
    expect(offlineText).toBeInTheDocument();
    expect(getByText("7.0% of fleet")).toBeInTheDocument();
    const offlineChart =
      offlineText.parentElement?.parentElement?.querySelector(chartSelector);
    expect(offlineChart).toBeInTheDocument();
    expect(offlineChart).toHaveClass(criticalClass);
  });
});
