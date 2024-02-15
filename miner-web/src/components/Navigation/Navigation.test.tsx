import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import "@testing-library/jest-dom";

import Navigation from "./Navigation";

vi.mock("react-router-dom", () => ({
  ...vi.importActual("react-router-dom"),
  Link: vi.fn().mockReturnValue(<div></div>),
  useLocation: () => ({
    pathname: "localhost:3000/example/path",
  }),
}));

describe("Navigation", () => {
  const mockOnClickReboot = vi.fn();
  const mockOnClickSleep = vi.fn();

  const mockProps = {
    controller_ip: {
      loading: false,
      value: "192.168.0.1",
    },
    controller_mac: {
      loading: false,
      value: "00.11.22.33.44.55",
    },
    hashboard_serials: {
      loading: false,
      value: ["ABC123", "DEF456"],
    },
    pool_info: {
      loading: false,
      url: "stratum+tcp://host.docker.internal:3333",
      worker: "worker1",
    },
    onClickReboot: mockOnClickReboot,
    onClickSleep: mockOnClickSleep,
  };

  test("renders the navigation component with correct props", () => {
    render(<Navigation {...mockProps} />);

    // Assert that the controller IP is rendered correctly
    const controllerIpElement = screen.getByText(mockProps.controller_ip.value);
    expect(controllerIpElement).toBeInTheDocument();

    // Assert that the controller MAC is rendered correctly
    const controllerMacElement = screen.getByText("00:11:22:33:44:55");
    expect(controllerMacElement).toBeInTheDocument();

    // Assert that the hashboard serials are rendered correctly
    const hashboardSerialsElement = screen.getByText("ABC1 23");
    expect(hashboardSerialsElement).toBeInTheDocument();
    const CaretIconElement = screen.queryByTestId("caret-icon");
    expect(CaretIconElement).toBeDefined();

    // Assert that the pool info is rendered correctly
    const poolInfoElement = screen.getByText("host.docker.internal");
    expect(poolInfoElement).toBeInTheDocument();

    // Assert that the reboot button is rendered correctly
    const rebootButton = screen.getByRole("button", { name: "Reboot" });
    expect(rebootButton).toBeInTheDocument();

    // Assert that the sleep button is rendered correctly
    const sleepButton = screen.getByRole("button", { name: "Sleep" });
    expect(sleepButton).toBeInTheDocument();
  });

  test("renders single hashboard without a caret icon", () => {
    render(
      <Navigation
        {...mockProps}
        hashboard_serials={{
          loading: mockProps.hashboard_serials.loading,
          value: [mockProps.hashboard_serials.value[0]],
        }}
      />
    );

    const hashboardSerialsElement = screen.getByText("ABC1 23");
    expect(hashboardSerialsElement).toBeInTheDocument();
    const CaretIconElement = screen.queryByTestId("caret-icon");
    expect(CaretIconElement).toBeNull();
  });

  test("calls the onClickReboot function when the reboot button is clicked", () => {
    render(<Navigation {...mockProps} />);

    const rebootButton = screen.getByRole("button", { name: "Reboot" });
    fireEvent.click(rebootButton);

    expect(mockOnClickReboot).toHaveBeenCalledTimes(1);
  });

  test("calls the onClickSleep function when the sleep button is clicked", () => {
    render(<Navigation {...mockProps} />);

    const sleepButton = screen.getByRole("button", { name: "Sleep" });
    fireEvent.click(sleepButton);

    expect(mockOnClickSleep).toHaveBeenCalledTimes(1);
  });
});
