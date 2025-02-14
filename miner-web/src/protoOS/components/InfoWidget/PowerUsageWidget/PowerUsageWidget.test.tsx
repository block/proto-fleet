import { fireEvent, render, within } from "@testing-library/react";
import { describe, expect, test } from "vitest";

import { mockPowerData } from "./constants";
import { convertAggregatePowerValues, convertPowerValues } from "./utility";
import PowerUsageWidget from ".";
import { getDisplayValue } from "@/shared/utils/stringUtils";
import { convertWtoKW } from "@/shared/utils/utility";

describe("PowerUsageWidget", () => {
  const powerAggregates = convertAggregatePowerValues(mockPowerData.aggregates);
  const avgPower = getDisplayValue(powerAggregates?.avg);
  const power = getDisplayValue(
    convertWtoKW(mockPowerData.data[mockPowerData.data.length - 1].value)
  );
  const duration = "12h";
  const avgPowerLabel = `${duration.toUpperCase()} avg. power usage`;
  const avgPowerDisplay = `${avgPower} kW`;
  const powerDisplay = `${power} kW`;
  const powerValues = convertPowerValues(mockPowerData.data);

  test("renders the widget in loading state", () => {
    const { getByTestId, queryByTestId, queryByText } = render(
      <PowerUsageWidget
        loading
        powerAggregates={powerAggregates}
        powerValues={powerValues}
        duration={duration}
      />
    );
    expect(getByTestId("skeleton-bar")).toBeInTheDocument();
    expect(getByTestId("bar")).toBeInTheDocument();
    expect(queryByText(powerDisplay)).not.toBeInTheDocument();
    expect(queryByTestId("empty-value")).not.toBeInTheDocument();
  });

  test("renders the widget in empty state", () => {
    const { getByTestId, queryByTestId, queryByText } = render(
      <PowerUsageWidget duration={duration} />
    );
    expect(queryByTestId("skeleton-bar")).not.toBeInTheDocument();
    expect(getByTestId("bar")).toBeInTheDocument();
    expect(queryByText(powerDisplay)).not.toBeInTheDocument();
    expect(getByTestId("empty-value")).toBeInTheDocument();
  });

  test("renders the widget with value", () => {
    const { getByTestId, getByText, queryByTestId } = render(
      <PowerUsageWidget
        powerAggregates={powerAggregates}
        powerValues={powerValues}
        duration={duration}
      />
    );
    expect(queryByTestId("skeleton-bar")).not.toBeInTheDocument();
    expect(getByTestId("bar")).toBeInTheDocument();
    expect(getByText(powerDisplay)).toBeInTheDocument();
    expect(queryByTestId("empty-value")).not.toBeInTheDocument();
  });

  test("does not open the modal if loading", () => {
    const { getByTestId, queryByTestId } = render(
      <PowerUsageWidget
        loading
        powerAggregates={powerAggregates}
        powerValues={powerValues}
        duration={duration}
      />
    );
    fireEvent.click(getByTestId("info-widget"));
    expect(queryByTestId("modal")).not.toBeInTheDocument();
  });

  test("opens the modal if empty", () => {
    const { getByTestId } = render(<PowerUsageWidget duration={duration} />);
    fireEvent.click(getByTestId("info-widget"));
    const modal = getByTestId("modal");
    expect(modal).toBeInTheDocument();

    const { queryByText } = within(modal);
    expect(queryByText(powerDisplay)).not.toBeInTheDocument();
    expect(queryByText(avgPowerDisplay)).not.toBeInTheDocument();
  });

  test("opens the modal with value", () => {
    const { getByTestId } = render(
      <PowerUsageWidget
        powerAggregates={powerAggregates}
        powerValues={powerValues}
        duration={duration}
      />
    );
    fireEvent.click(getByTestId("info-widget"));
    const modal = getByTestId("modal");
    expect(modal).toBeInTheDocument();

    const { getByText } = within(modal);
    expect(getByText(powerDisplay)).toBeInTheDocument();
    expect(getByText(avgPowerLabel)).toBeInTheDocument();
    expect(getByText(avgPowerDisplay)).toBeInTheDocument();
  });
});
