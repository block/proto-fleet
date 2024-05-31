import { fireEvent, render, within } from "@testing-library/react";
import { describe, expect, test } from "vitest";

import { getDisplayValue } from "common/utils/stringUtils";

import { convertEfficiencyValues } from "./utility";
import EfficiencyWidget, { mockEfficiencyData } from ".";

describe("EfficiencyWidget", () => {
  const avgEfficiency = getDisplayValue(mockEfficiencyData.aggregates.avg);
  const efficiency = getDisplayValue(
    mockEfficiencyData.data[mockEfficiencyData.data.length - 1].value
  );
  const avgEfficiencyDisplay = `${avgEfficiency} J/TH`;
  const efficiencyDisplay = `${efficiency} J/TH`;
  const efficiencyValues = convertEfficiencyValues(mockEfficiencyData.data);

  test("renders the widget in loading state", () => {
    const { getByTestId, queryByTestId, queryByText } = render(
      <EfficiencyWidget
        loading
        avgEfficiency={avgEfficiency}
        efficiencyValues={efficiencyValues}
      />
    );
    expect(getByTestId("skeleton-bar")).toBeInTheDocument();
    expect(queryByTestId("line")).not.toBeInTheDocument();
    expect(queryByText(efficiencyDisplay)).not.toBeInTheDocument();
    expect(queryByTestId("empty-value")).not.toBeInTheDocument();
  });

  test("renders the widget in empty state", () => {
    const { getByTestId, queryByTestId, queryByText } = render(
      <EfficiencyWidget />
    );
    expect(queryByTestId("skeleton-bar")).not.toBeInTheDocument();
    expect(queryByTestId("line")).not.toBeInTheDocument();
    expect(queryByText(efficiencyDisplay)).not.toBeInTheDocument();
    expect(getByTestId("empty-value")).toBeInTheDocument();
  });

  test("renders the widget with value", () => {
    const { getByTestId, getByText, queryByTestId } = render(
      <EfficiencyWidget
        avgEfficiency={avgEfficiency}
        efficiencyValues={efficiencyValues}
      />
    );
    expect(queryByTestId("skeleton-bar")).not.toBeInTheDocument();
    expect(getByTestId("line")).toBeInTheDocument();
    expect(getByText(efficiencyDisplay)).toBeInTheDocument();
    expect(queryByTestId("empty-value")).not.toBeInTheDocument();
  });

  test("does not open the modal if loading", () => {
    const { getByTestId, queryByTestId } = render(
      <EfficiencyWidget
        loading
        avgEfficiency={avgEfficiency}
        efficiencyValues={efficiencyValues}
      />
    );
    fireEvent.click(getByTestId("info-widget"));
    expect(queryByTestId("modal")).not.toBeInTheDocument();
  });

  test("opens the modal if empty", () => {
    const { getByTestId } = render(<EfficiencyWidget />);
    fireEvent.click(getByTestId("info-widget"));
    const modal = getByTestId("modal");
    expect(modal).toBeInTheDocument();

    const { queryByText } = within(modal);
    expect(queryByText(efficiencyDisplay)).not.toBeInTheDocument();
    expect(queryByText(avgEfficiencyDisplay)).not.toBeInTheDocument();
  });

  test("opens the modal with value", () => {
    const { getByTestId } = render(
      <EfficiencyWidget
        avgEfficiency={avgEfficiency}
        efficiencyValues={efficiencyValues}
      />
    );
    fireEvent.click(getByTestId("info-widget"));
    const modal = getByTestId("modal");
    expect(modal).toBeInTheDocument();

    const { getByText } = within(modal);
    expect(getByText(efficiencyDisplay)).toBeInTheDocument();
    expect(getByText(avgEfficiencyDisplay)).toBeInTheDocument();
  });
});
