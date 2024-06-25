import { fireEvent, render, within } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";

import { getDisplayValue } from "common/utils/stringUtils";

import { mockTemperatureData } from "./constants";
import TempWidget from ".";

vi.mock("react-router-dom", () => ({
  ...vi.importActual("react-router-dom"),
  useNavigate: () => ({
    Navigation: vi.fn(),
  }),
}));

describe("TempWidget", () => {
  const temp =
    mockTemperatureData.data[mockTemperatureData.data.length - 1].value;
  const highestTemp = mockTemperatureData.aggregates?.max;
  const hashboardSerials = ["1", "2", "3"];
  const tempDisplay = `${getDisplayValue(temp)}°c`;

  test("renders the widget in loading state", () => {
    const { getByTestId, queryByTestId, queryByText } = render(
      <TempWidget
        loading
        temp={temp}
        duration="12h"
        highestTemp={highestTemp}
        hashboardSerials={hashboardSerials}
      />
    );
    expect(getByTestId("skeleton-bar")).toBeInTheDocument();
    expect(getByTestId("bar")).toBeInTheDocument();
    expect(queryByText(tempDisplay)).not.toBeInTheDocument();
    expect(queryByTestId("empty-value")).not.toBeInTheDocument();
  });

  test("renders the widget in empty state", () => {
    const { getByTestId, queryByTestId, queryByText } = render(
      <TempWidget />
    );
    expect(queryByTestId("skeleton-bar")).not.toBeInTheDocument();
    expect(getByTestId("bar")).toBeInTheDocument();
    expect(queryByText(tempDisplay)).not.toBeInTheDocument();
    expect(getByTestId("empty-value")).toBeInTheDocument();
  });

  test("renders the widget with value", () => {
    const { getByTestId, getByText, queryByTestId } = render(
      <TempWidget
        temp={temp}
        duration="12h"
        highestTemp={highestTemp}
        hashboardSerials={hashboardSerials}
      />
    );
    expect(queryByTestId("skeleton-bar")).not.toBeInTheDocument();
    expect(getByTestId("bar")).toBeInTheDocument();
    expect(getByText(tempDisplay)).toBeInTheDocument();
    expect(queryByTestId("empty-value")).not.toBeInTheDocument();
  });

  test("does not open the modal if loading", () => {
    const { getByTestId, queryByTestId } = render(
      <TempWidget
        loading
        temp={temp}
        duration="12h"
        highestTemp={highestTemp}
        hashboardSerials={hashboardSerials}
      />
    );
    fireEvent.click(getByTestId("info-widget"));
    expect(queryByTestId("modal")).not.toBeInTheDocument();
  });

  test("opens the modal if empty", () => {
    const { getByTestId } = render(<TempWidget hashboardSerials={hashboardSerials} />);
    fireEvent.click(getByTestId("info-widget"));
    const modal = getByTestId("modal");
    expect(modal).toBeInTheDocument();

    const { queryByText } = within(modal);
    expect(queryByText(tempDisplay)).not.toBeInTheDocument();
  });

  test("opens the modal with value", () => {
    const { getByTestId } = render(
      <TempWidget
        temp={temp}
        duration="12h"
        highestTemp={highestTemp}
        hashboardSerials={hashboardSerials}
      />
    );
    fireEvent.click(getByTestId("info-widget"));
    const modal = getByTestId("modal");
    expect(modal).toBeInTheDocument();

    const { getAllByText } = within(modal);
    expect(getAllByText(tempDisplay)).toHaveLength(2);
  });
});
