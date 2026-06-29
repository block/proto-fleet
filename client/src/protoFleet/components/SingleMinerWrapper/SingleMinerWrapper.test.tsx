import { MemoryRouter, Route, Routes } from "react-router-dom";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import SingleMinerWrapper from "./SingleMinerWrapper";

describe("SingleMinerWrapper", () => {
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
