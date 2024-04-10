import { fireEvent, render, within } from "@testing-library/react";
import { describe, expect, test } from "vitest";

import EfficiencyWidget from ".";

describe("PowerUsageWidget", () => {
  const efficiency = "15.50";
  const efficiencyDisplay = `${efficiency} J/TH`;
  const efficiencyValues = [
    { value: 1 },
    { value: 3 },
    { value: 2 },
    { value: 9 },
    { value: 5 },
  ];

  test("renders the widget in loading state", () => {
    const { getByTestId, queryByTestId, queryByText } = render(
      <EfficiencyWidget
        loading
        efficiency={efficiency}
        efficiencyValues={efficiencyValues}
      />
    );
    expect(getByTestId("skeleton-bar")).toBeInTheDocument();
    expect(getByTestId("line")).toBeInTheDocument();
    expect(queryByText(efficiencyDisplay)).not.toBeInTheDocument();
    expect(queryByTestId("empty-value")).not.toBeInTheDocument();
  });

  test("renders the widget in empty state", () => {
    const { getByTestId, queryByTestId, queryByText } = render(
      <EfficiencyWidget />
    );
    expect(queryByTestId("skeleton-bar")).not.toBeInTheDocument();
    expect(getByTestId("line")).toBeInTheDocument();
    expect(queryByText(efficiencyDisplay)).not.toBeInTheDocument();
    expect(getByTestId("empty-value")).toBeInTheDocument();
  });

  test("renders the widget with value", () => {
    const { getByTestId, getByText, queryByTestId } = render(
      <EfficiencyWidget
        efficiency={efficiency}
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
        efficiency={efficiency}
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
  });

  test("opens the modal with value", () => {
    const { getByTestId } = render(
      <EfficiencyWidget
        efficiency={efficiency}
        efficiencyValues={efficiencyValues}
      />
    );
    fireEvent.click(getByTestId("info-widget"));
    const modal = getByTestId("modal");
    expect(modal).toBeInTheDocument();

    const { getByText } = within(modal);
    expect(getByText(efficiencyDisplay)).toBeInTheDocument();
  });
});
