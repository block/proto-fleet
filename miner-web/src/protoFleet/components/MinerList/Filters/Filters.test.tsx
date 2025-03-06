import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { Miner } from "../types";
import Filters from "./Filters";

describe("Filters", () => {
  const mockMiners = [
    {
      name: "Miner 1",
      status: {
        hashing: true,
        broken: false,
        offline: false,
        asleep: false,
      },
    },
    {
      name: "Miner 2",
      status: {
        hashing: false,
        broken: true,
        offline: false,
        asleep: false,
      },
    },
    {
      name: "Miner 3",
      status: {
        hashing: false,
        broken: false,
        offline: true,
        asleep: false,
      },
    },
  ];

  it("changes active filter when clicking filter buttons", () => {
    const setFilteredMiners = vi.fn();
    render(
      <Filters
        miners={mockMiners as Miner[]}
        setFilteredMiners={setFilteredMiners}
      />,
    );

    // Initially "All Miners" should be active
    expect(screen.getByText("All Miners").closest("button")).toHaveClass(
      "bg-core-primary-fill",
    );

    // Click "Hashing" filter
    fireEvent.click(screen.getByText("Hashing"));

    // "Hashing" should now be active and "All Miners" should be inactive
    expect(screen.getByText("Hashing").closest("button")).toHaveClass(
      "bg-core-primary-fill",
    );
    expect(screen.getByText("All Miners").closest("button")).toHaveClass(
      "bg-surface-default",
    );
  });

  it("displays correct count for each filter status", () => {
    const setFilteredMiners = vi.fn();
    render(
      <Filters
        miners={mockMiners as Miner[]}
        setFilteredMiners={setFilteredMiners}
      />,
    );

    expect(
      screen.getByText("Hashing").querySelector("span")?.innerHTML,
    ).toEqual("1");
    expect(screen.getByText("Broken").querySelector("span")?.innerHTML).toEqual(
      "1",
    );
    expect(
      screen.getByText("Offline").querySelector("span")?.innerHTML,
    ).toEqual("1");
    expect(screen.getByText("Asleep").querySelector("span")?.innerHTML).toEqual(
      "0",
    );
  });
});
