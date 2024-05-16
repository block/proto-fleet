import { fireEvent, render, within } from "@testing-library/react";
import { describe, expect, test } from "vitest";

import { getDisplayValue } from "common/utils/stringUtils";

import PowerUsageWidget from ".";

describe("PowerUsageWidget", () => {
  const powerUsage = "3.10";
  const powerUsageDisplay = `${getDisplayValue(powerUsage)} kW`;

  test("renders the widget in loading state", () => {
    const { getByTestId, queryByTestId, queryByText } = render(
      <PowerUsageWidget loading powerUsage={powerUsage} />
    );
    expect(getByTestId("skeleton-bar")).toBeInTheDocument();
    expect(getByTestId("bar")).toBeInTheDocument();
    expect(queryByText(powerUsageDisplay)).not.toBeInTheDocument();
    expect(queryByTestId("empty-value")).not.toBeInTheDocument();
  });

  test("renders the widget in empty state", () => {
    const { getByTestId, queryByTestId, queryByText } = render(
      <PowerUsageWidget />
    );
    expect(queryByTestId("skeleton-bar")).not.toBeInTheDocument();
    expect(getByTestId("bar")).toBeInTheDocument();
    expect(queryByText(powerUsageDisplay)).not.toBeInTheDocument();
    expect(getByTestId("empty-value")).toBeInTheDocument();
  });

  test("renders the widget with value", () => {
    const { getByTestId, getByText, queryByTestId } = render(
      <PowerUsageWidget powerUsage={powerUsage} />
    );
    expect(queryByTestId("skeleton-bar")).not.toBeInTheDocument();
    expect(getByTestId("bar")).toBeInTheDocument();
    expect(getByText(powerUsageDisplay)).toBeInTheDocument();
    expect(queryByTestId("empty-value")).not.toBeInTheDocument();
  });

  test("does not open the modal if loading", () => {
    const { getByTestId, queryByTestId } = render(
      <PowerUsageWidget loading powerUsage={powerUsage} />
    );
    fireEvent.click(getByTestId("info-widget"));
    expect(queryByTestId("modal")).not.toBeInTheDocument();
  });

  test("opens the modal if empty", () => {
    const { getByTestId } = render(<PowerUsageWidget />);
    fireEvent.click(getByTestId("info-widget"));
    const modal = getByTestId("modal");
    expect(modal).toBeInTheDocument();

    const { queryByText } = within(modal);
    expect(queryByText(powerUsageDisplay)).not.toBeInTheDocument();
  });

  test("opens the modal with value", () => {
    const { getByTestId } = render(
      <PowerUsageWidget powerUsage={powerUsage} />
    );
    fireEvent.click(getByTestId("info-widget"));
    const modal = getByTestId("modal");
    expect(modal).toBeInTheDocument();

    const { getByText } = within(modal);
    expect(getByText(powerUsageDisplay)).toBeInTheDocument();
  });
});
