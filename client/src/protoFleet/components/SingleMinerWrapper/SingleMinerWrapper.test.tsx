import { useEffect } from "react";
import { MemoryRouter, Route, Routes, useNavigate } from "react-router-dom";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import SingleMinerWrapper from "./SingleMinerWrapper";

describe("SingleMinerWrapper", () => {
  it("remounts the hosted subtree when the miner id changes", () => {
    const onMount = vi.fn();
    const Child = () => {
      useEffect(() => {
        onMount();
      }, []);
      return <div data-testid="embedded-child" />;
    };
    const Switcher = () => {
      const navigate = useNavigate();
      return <button onClick={() => navigate("/miners/miner-2/hashrate")}>switch</button>;
    };

    render(
      <MemoryRouter initialEntries={["/miners/miner-1/hashrate"]}>
        <Switcher />
        <Routes>
          <Route
            path="/miners/:id/*"
            element={
              <SingleMinerWrapper>
                <Child />
              </SingleMinerWrapper>
            }
          />
        </Routes>
      </MemoryRouter>,
    );

    expect(onMount).toHaveBeenCalledTimes(1);

    // Same route element, only the :id param changes — without the safeId key
    // the child would stay mounted (keeping miner A's local state).
    fireEvent.click(screen.getByText("switch"));
    expect(onMount).toHaveBeenCalledTimes(2);
  });

  it("keeps an opaque embedded view surface behind the animated content", () => {
    render(
      <MemoryRouter
        initialEntries={[
          {
            pathname: "/miners/miner-1/hashrate",
            state: { singleMinerMetadata: { minerName: "Rig Alpha" } },
          },
        ]}
      >
        <Routes>
          <Route
            path="/miners/:id/*"
            element={
              <SingleMinerWrapper>
                <div>Embedded miner content</div>
              </SingleMinerWrapper>
            }
          />
        </Routes>
      </MemoryRouter>,
    );

    expect(screen.getByTestId("single-miner-surface")).toHaveClass("min-h-screen", "bg-surface-base");
    expect(screen.getByTestId("single-miner-content")).toHaveClass("min-h-screen", "bg-surface-base");
  });
});
