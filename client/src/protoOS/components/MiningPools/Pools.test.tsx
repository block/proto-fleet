import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import Pools from "./Pools";

vi.mock("@/protoOS/api", () => ({
  useTestConnection: () => ({
    pending: false,
    testConnection: vi.fn(),
  }),
}));

describe("ProtoOS Pools", () => {
  it("uses the URL as the fallback title and renders the full username below it", () => {
    render(
      <Pools
        onChangePools={vi.fn()}
        pools={[
          {
            name: "",
            url: "stratum+tcp://mine.ocean.xyz:3334",
            username: "mann23.workerbee",
            password: "",
            priority: 0,
          },
          {
            name: "",
            url: "",
            username: "",
            password: "",
            priority: 1,
          },
          {
            name: "",
            url: "",
            username: "",
            password: "",
            priority: 2,
          },
        ]}
      />,
    );

    expect(screen.getAllByText("stratum+tcp://mine.ocean.xyz:3334")).toHaveLength(2);
    expect(screen.getByText("mann23.workerbee")).toBeInTheDocument();
  });

  it("renders the full username when it contains dots", () => {
    render(
      <Pools
        onChangePools={vi.fn()}
        pools={[
          {
            name: "",
            url: "stratum+tcp://mine.ocean.xyz:3334",
            username: "alice.main.worker-01",
            password: "",
            priority: 0,
          },
          {
            name: "",
            url: "",
            username: "",
            password: "",
            priority: 1,
          },
          {
            name: "",
            url: "",
            username: "",
            password: "",
            priority: 2,
          },
        ]}
      />,
    );

    expect(screen.getByText("alice.main.worker-01")).toBeInTheDocument();
  });
});
