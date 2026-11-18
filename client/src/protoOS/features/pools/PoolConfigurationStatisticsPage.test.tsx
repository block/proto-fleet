import { MemoryRouter } from "react-router-dom";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";
import PoolConfigurationStatisticsPage from "./PoolConfigurationStatisticsPage";
import type { PoolConfigurationStatisticsService } from "@/protoOS/api/adapters/poolStatistics";
import { asPoolId } from "@/protoOS/store/contract";

const mode = vi.hoisted(() => ({ enabled: true, error: undefined as string | undefined }));
beforeEach(() => {
  mode.enabled = true;
  mode.error = undefined;
});
const items = ["Client A", "Client B"].map((name, index) => ({
  id: String(index),
  name,
  revision: "1",
  assignedCount: 3,
  pools: [
    {
      id: asPoolId(`pool:${10 + index}`),
      name: "Primary",
      url: "stratum+tcp://same.pool:3333",
      username: index ? "account.b.worker" : "account.a.worker",
      priority: 0,
      status: "Active",
    },
  ],
}));
vi.mock("./usePoolConfigurationStatistics", async (original) => ({
  ...(await original<typeof import("./usePoolConfigurationStatistics")>()),
  usePoolConfigurationStatistics: (_service: unknown, _duration: unknown, _page: unknown, id?: string) => ({
    enabled: mode.enabled,
    error: mode.error,
    configurations: items,
    selected: items.find((item) => item.id === id),
    readings: id ? items.filter((item) => item.id === id) : items,
    history: {
      byPool: {
        "pool:10": { accepted: 9, rejected: 1, acceptanceRate: 0.9, partial: false },
        "pool:11": { accepted: 1, rejected: 9, acceptanceRate: 0.1, partial: false },
      },
      unavailable: [],
    },
    page: 0,
  }),
}));
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({ useMinerHosting: () => ({ minerRoot: "/miners/ct1" }) }));
vi.mock("@/shared/hooks/useWindowDimensions", () => ({ useWindowDimensions: () => ({ isPhone: false }) }));
const service = {} as PoolConfigurationStatisticsService;
test("summarizes configurations separately, then opens only the selected configuration", () => {
  render(
    <MemoryRouter initialEntries={["/miners/ct1/pools?keep=1"]}>
      <PoolConfigurationStatisticsPage service={service} legacy={<p>Legacy</p>} />
    </MemoryRouter>,
  );
  expect(screen.getByText("90.0%")).toBeInTheDocument();
  expect(screen.getByText("10.0%")).toBeInTheDocument();
  expect(screen.queryByRole("region", { name: "Primary" })).not.toBeInTheDocument();
  fireEvent.click(within(screen.getByRole("row", { name: /Client A/ })).getByText("90.0%"));
  const detail = screen.getByRole("region", { name: "Primary" });
  expect(within(detail).getByText("Username: account.a.worker")).toBeInTheDocument();
  expect(screen.queryByText("Username: account.b.worker")).not.toBeInTheDocument();
  expect(screen.getByRole("link", { name: "Configure pools" })).toHaveAttribute(
    "href",
    "/miners/ct1/settings/mining-pools",
  );
  const breadcrumb = screen.getByRole("navigation", { name: "Breadcrumb" });
  expect(breadcrumb).toHaveTextContent("Pools/Client A");
  const poolsLink = within(breadcrumb).getByRole("link", { name: "Pools" });
  expect(poolsLink).toHaveAttribute("href", "/miners/ct1/pools?keep=1");
  fireEvent.click(poolsLink);
  expect(screen.getByRole("row", { name: /Client B/ })).toBeInTheDocument();
  expect(screen.queryByRole("navigation", { name: "Breadcrumb" })).not.toBeInTheDocument();
});

test("an uncertain mode read does not present legacy-only statistics as complete", () => {
  mode.enabled = false;
  mode.error = "Could not refresh configurations.";
  render(
    <MemoryRouter>
      <PoolConfigurationStatisticsPage service={service} legacy={<p>Legacy statistics</p>} />
    </MemoryRouter>,
  );
  expect(screen.queryByText("Legacy statistics")).not.toBeInTheDocument();
  expect(screen.getByRole("alert")).toHaveTextContent(mode.error);
});
