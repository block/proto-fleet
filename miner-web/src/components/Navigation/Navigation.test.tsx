import { render, screen } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";

import Navigation from "./Navigation";

vi.mock("react-router-dom", () => ({
  ...vi.importActual("react-router-dom"),
  Link: vi.fn().mockReturnValue(<div></div>),
  useLocation: () => ({
    pathname: "localhost:3000/example/path",
  }),
}));

describe("Navigation", () => {
  const mockProps = {
    macInfo: {
      loading: false,
      value: "00.11.22.33.44.55",
    },
    poolInfo: {
      loading: false,
      url: "stratum+tcp://host.docker.internal:3333",
      worker: "worker1",
    },
  };

  test("renders the navigation component with correct props", () => {
    render(<Navigation {...mockProps} />);

    // Assert that the controller MAC is rendered correctly
    const controllerMacElement = screen.getByText("00:11:22:33:44:55");
    expect(controllerMacElement).toBeInTheDocument();

    // Assert that the pool info is rendered correctly
    const poolInfoElement = screen.getByText("host.docker.internal");
    expect(poolInfoElement).toBeInTheDocument();
  });
});
