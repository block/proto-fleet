import { fireEvent, render, within } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";

import { getDisplayValue } from "common/utils/stringUtils";

import AsicTempWidget from ".";

vi.mock("react-router-dom", () => ({
  ...vi.importActual("react-router-dom"),
  useNavigate: () => ({
    Navigation: vi.fn(),
  }),
}));

describe("AsicTempWidget", () => {
  const asicTemp = "1300";
  const asicTempDisplay = `${getDisplayValue(asicTemp)}°c`;

  test("renders the widget in loading state", () => {
    const { getByTestId, queryByTestId, queryByText } = render(
      <AsicTempWidget loading asicTemp={asicTemp} />
    );
    expect(getByTestId("skeleton-bar")).toBeInTheDocument();
    expect(getByTestId("bar")).toBeInTheDocument();
    expect(queryByText(asicTempDisplay)).not.toBeInTheDocument();
    expect(queryByTestId("empty-value")).not.toBeInTheDocument();
  });

  test("renders the widget in empty state", () => {
    const { getByTestId, queryByTestId, queryByText } = render(
      <AsicTempWidget />
    );
    expect(queryByTestId("skeleton-bar")).not.toBeInTheDocument();
    expect(getByTestId("bar")).toBeInTheDocument();
    expect(queryByText(asicTempDisplay)).not.toBeInTheDocument();
    expect(getByTestId("empty-value")).toBeInTheDocument();
  });

  test("renders the widget with value", () => {
    const { getByTestId, getByText, queryByTestId } = render(
      <AsicTempWidget asicTemp={asicTemp} />
    );
    expect(queryByTestId("skeleton-bar")).not.toBeInTheDocument();
    expect(getByTestId("bar")).toBeInTheDocument();
    expect(getByText(asicTempDisplay)).toBeInTheDocument();
    expect(queryByTestId("empty-value")).not.toBeInTheDocument();
  });

  test("does not open the modal if loading", () => {
    const { getByTestId, queryByTestId } = render(
      <AsicTempWidget loading asicTemp={asicTemp} />
    );
    fireEvent.click(getByTestId("info-widget"));
    expect(queryByTestId("modal")).not.toBeInTheDocument();
  });

  test("opens the modal if empty", () => {
    const { getByTestId } = render(<AsicTempWidget />);
    fireEvent.click(getByTestId("info-widget"));
    const modal = getByTestId("modal");
    expect(modal).toBeInTheDocument();

    const { queryByText } = within(modal);
    expect(queryByText(asicTempDisplay)).not.toBeInTheDocument();
  });

  test("opens the modal with value", () => {
    const { getByTestId } = render(<AsicTempWidget asicTemp={asicTemp} />);
    fireEvent.click(getByTestId("info-widget"));
    const modal = getByTestId("modal");
    expect(modal).toBeInTheDocument();

    const { getByText } = within(modal);
    expect(getByText(asicTempDisplay)).toBeInTheDocument();
  });
});
