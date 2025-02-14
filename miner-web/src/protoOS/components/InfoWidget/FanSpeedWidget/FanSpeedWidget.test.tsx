import { fireEvent, render, within } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";

import FanSpeedWidget from ".";
import { FanInfo } from "@/protoOS/api/types";

import { getDisplayValue } from "@/shared/utils/stringUtils";

vi.mock("react-router-dom", () => ({
  ...vi.importActual("react-router-dom"),
  useNavigate: () => ({
    Navigation: vi.fn(),
  }),
}));

describe("FanSpeedWidget", () => {
  const fanSpeeds: FanInfo[] = [{ rpm: 1200 }];
  const fanSpeedDisplay = `${getDisplayValue(fanSpeeds[0].rpm)} RPM`;

  test("renders the widget in loading state", () => {
    const { getByTestId, queryByTestId, queryByText } = render(
      <FanSpeedWidget loading fanSpeeds={fanSpeeds} />
    );
    expect(getByTestId("skeleton-bar")).toBeInTheDocument();
    expect(queryByTestId("fan-speed-pie-chart")).not.toBeInTheDocument();
    expect(queryByText(fanSpeedDisplay)).not.toBeInTheDocument();
    expect(queryByTestId("empty-value")).not.toBeInTheDocument();
  });

  test("renders the widget in empty state", () => {
    const { getByTestId, queryByTestId, queryByText } = render(
      <FanSpeedWidget />
    );
    expect(queryByTestId("skeleton-bar")).not.toBeInTheDocument();
    expect(queryByTestId("fan-speed-pie-chart")).not.toBeInTheDocument();
    expect(queryByText(fanSpeedDisplay)).not.toBeInTheDocument();
    expect(getByTestId("empty-value")).toBeInTheDocument();
  });

  test("renders the widget with value", () => {
    const { getByTestId, getByText, queryByTestId } = render(
      <FanSpeedWidget fanSpeeds={fanSpeeds} />
    );
    expect(queryByTestId("skeleton-bar")).not.toBeInTheDocument();
    expect(getByTestId("fan-speed-pie-chart")).toBeInTheDocument();
    expect(getByText(fanSpeedDisplay)).toBeInTheDocument();
    expect(queryByTestId("empty-value")).not.toBeInTheDocument();
  });

  test("does not open the modal if loading", () => {
    const { getByTestId, queryByTestId } = render(
      <FanSpeedWidget loading fanSpeeds={fanSpeeds} />
    );
    fireEvent.click(getByTestId("info-widget"));
    expect(queryByTestId("modal")).not.toBeInTheDocument();
  });

  test("opens the modal if empty", () => {
    const { getByTestId, queryByTestId } = render(<FanSpeedWidget />);
    fireEvent.click(getByTestId("info-widget"));
    const modal = getByTestId("modal");
    expect(modal).toBeInTheDocument();

    const { queryByText } = within(modal);
    expect(queryByText(fanSpeedDisplay)).not.toBeInTheDocument();
    expect(queryByTestId("fan-speed-pie-chart")).not.toBeInTheDocument();
  });

  test("opens the modal with value", () => {
    const { getByTestId, queryAllByTestId } = render(
      <FanSpeedWidget fanSpeeds={fanSpeeds} />
    );
    fireEvent.click(getByTestId("info-widget"));
    const modal = getByTestId("modal");
    expect(modal).toBeInTheDocument();

    const { getByText } = within(modal);
    expect(getByText(`Low speed • ${fanSpeedDisplay}`)).toBeInTheDocument();
    expect(queryAllByTestId("fan-speed-pie-chart")).toHaveLength(2);
  });
});
