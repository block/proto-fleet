import { render, screen } from "@testing-library/react";
import { describe, expect, test } from "vitest";

import ReadOnlyMiningPools from "./ReadOnlyMiningPools";
import { PoolInfo } from "@/shared/components/MiningPools/types";

const pool = (overrides: Partial<PoolInfo>): PoolInfo => ({
  name: "",
  url: "",
  username: "",
  password: "",
  priority: 0,
  ...overrides,
});

describe("ReadOnlyMiningPools", () => {
  test("shows configured pools without any edit affordance", () => {
    const pools: PoolInfo[] = [
      pool({ name: "Primary", url: "stratum+tcp://pool.example:3333", username: "worker1", priority: 0 }),
      pool({ priority: 1 }),
      pool({ priority: 2 }),
    ];

    render(<ReadOnlyMiningPools pools={pools} />);

    expect(screen.getByText("Primary")).toBeInTheDocument();
    expect(screen.getByText("stratum+tcp://pool.example:3333")).toBeInTheDocument();
    expect(screen.getByText("worker1")).toBeInTheDocument();
    // Read-only: no Update / Add / reorder controls.
    expect(screen.queryByText("Update")).not.toBeInTheDocument();
    expect(screen.queryByText(/Add (another )?pool/i)).not.toBeInTheDocument();
    expect(screen.getByText(/managed from Fleet/i)).toBeInTheDocument();
  });

  test("renders an empty state when no pools are configured", () => {
    render(<ReadOnlyMiningPools pools={[pool({ priority: 0 }), pool({ priority: 1 }), pool({ priority: 2 })]} />);

    expect(screen.getByTestId("read-only-pools-empty")).toBeInTheDocument();
    expect(screen.queryByText("Update")).not.toBeInTheDocument();
  });
});
