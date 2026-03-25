import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import PoolRow from "./PoolRow";

describe("PoolRow", () => {
  it("renders an extra subtitle line for the worker name", () => {
    render(
      <PoolRow
        poolIndex={0}
        pools={[
          {
            name: "Client pool A1",
            url: "stratum+tcp://mine.ocean.xyz:3334",
            username: "mann23.workerbee",
            password: "",
            priority: 0,
          },
        ]}
        subtitleExtra="mann23.workerbee"
        onClick={vi.fn()}
      />,
    );

    expect(screen.getByText("Client pool A1")).toBeInTheDocument();
    expect(screen.getByTestId("pool-0-saved-url")).toHaveTextContent("stratum+tcp://mine.ocean.xyz:3334");
    expect(screen.getByTestId("pool-0-saved-username")).toHaveTextContent("mann23.workerbee");
  });

  it("does not duplicate the title when the extra subtitle matches it", () => {
    render(
      <PoolRow
        poolIndex={0}
        pools={[
          {
            name: "",
            url: "stratum+tcp://mine.ocean.xyz:3334",
            username: "mann23.workerbee",
            password: "",
            priority: 0,
          },
        ]}
        subtitleExtra="mann23.workerbee"
        onClick={vi.fn()}
      />,
    );

    expect(screen.getByText("mann23.workerbee")).toBeInTheDocument();
    expect(screen.getByTestId("pool-0-saved-url")).toHaveTextContent("stratum+tcp://mine.ocean.xyz:3334");
    expect(screen.queryByTestId("pool-0-saved-username")).not.toBeInTheDocument();
  });
});
