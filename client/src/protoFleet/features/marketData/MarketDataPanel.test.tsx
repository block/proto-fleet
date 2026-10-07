import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import userEvent from "@testing-library/user-event";
import MarketDataPanel from "./MarketDataPanel";
import {
  GetMarketDataResponseSchema,
  MarketMetricSchema,
} from "@/protoFleet/api/generated/marketdata/v1/marketdata_pb";

const now = new Date("2026-09-30T12:00:00Z").getTime();
const metric = (value: number, source: string, minutesAgo = 0, stale = false) =>
  create(MarketMetricSchema, {
    value,
    source,
    stale,
    retrievedAt: timestampFromDate(new Date(now - minutesAgo * 60_000)),
  });
const data = () =>
  create(GetMarketDataResponseSchema, {
    enabled: true,
    bitcoinPriceUsd: metric(100000.25, "Coinbase"),
    estimatedHashpriceUsdPerPhDay: metric(65.38, "Coinbase + mempool.space"),
    networkHashrateHs: metric(1.01e21, "mempool.space"),
  });
const props = () => ({ data: data(), error: false, now, onRetry: vi.fn() });

describe("MarketDataPanel", () => {
  beforeEach(() => {
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (this: HTMLElement) {
      if (this.querySelector("[data-testid='market-data-info']")) return new DOMRect(180, 24, 32, 32);
      if (this.dataset.testid === "market-data-info-popover") return new DOMRect(0, 0, 320, 300);
      return new DOMRect();
    });
  });

  afterEach(() => vi.restoreAllMocks());

  it("formats all values with their units without visible explanations or source labels", () => {
    render(<MarketDataPanel {...props()} />);
    expect(screen.getByText("$100,000.25")).toBeInTheDocument();
    expect(screen.getByText("$65.38")).toBeInTheDocument();
    expect(screen.getByText("1,010")).toBeInTheDocument();
    expect(screen.getByText(/USD\/PH\/day/)).toBeInTheDocument();
    expect(screen.getByText(/EH\/s/)).toBeInTheDocument();
    expect(screen.getAllByText("<1 min ago")).toHaveLength(3);
    expect(screen.queryByText(/Hashprice estimates/)).not.toBeInTheDocument();
    expect(screen.queryByText(/Checks every minute/)).not.toBeInTheDocument();
    expect(screen.queryByText(/Coinbase|CoinGecko|mempool/)).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "About market data" })).toHaveAttribute("aria-expanded", "false");
  });

  it("shows estimation and delay details on click, and closes on a second click", () => {
    render(<MarketDataPanel {...props()} />);
    const trigger = screen.getByRole("button", { name: "About market data" });
    fireEvent.click(trigger);
    const explanation = screen.getByRole("region", { name: "About market data" });
    expect(trigger).toHaveAttribute("aria-expanded", "true");
    expect(trigger).toHaveAttribute("aria-controls", explanation.id);
    expect(screen.getByText(/excludes pool fees and/)).toBeInTheDocument();
    expect(screen.getByText(/last 144 blocks/)).toBeInTheDocument();
    expect(screen.getByText(/1,008 blocks/)).toBeInTheDocument();
    expect(screen.getByText(/retrieved, not published/)).toBeInTheDocument();
    expect(screen.queryByText(/Coinbase|CoinGecko|mempool/)).not.toBeInTheDocument();
    fireEvent.click(trigger);
    expect(screen.queryByRole("region", { name: "About market data" })).not.toBeInTheDocument();
    expect(trigger).toHaveAttribute("aria-expanded", "false");
  });

  it("opens from the keyboard and dismisses with Escape or an outside click", async () => {
    const user = userEvent.setup();
    render(<MarketDataPanel {...props()} />);
    const trigger = screen.getByRole("button", { name: "About market data" });
    await user.tab();
    expect(trigger).toHaveFocus();
    await user.keyboard("{Enter}");
    expect(screen.getByRole("region", { name: "About market data" })).toBeInTheDocument();
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("region", { name: "About market data" })).not.toBeInTheDocument();
    expect(trigger).toHaveFocus();
    await user.keyboard(" ");
    expect(screen.getByRole("region", { name: "About market data" })).toBeInTheDocument();
    await user.click(document.body);
    expect(screen.queryByRole("region", { name: "About market data" })).not.toBeInTheDocument();
  });

  it("renders the explanation as a dismissible bottom sheet on phones", () => {
    const originalWidth = window.innerWidth;
    const originalBreakpoint = document.body.style.getPropertyValue("--phone-max-width");
    document.body.style.setProperty("--phone-max-width", "631");
    Object.defineProperty(window, "innerWidth", { configurable: true, writable: true, value: 390 });

    try {
      render(<MarketDataPanel {...props()} />);
      fireEvent.click(screen.getByRole("button", { name: "About market data" }));
      expect(screen.getByRole("region", { name: "About market data" })).toBeInTheDocument();
      fireEvent.click(screen.getByTestId("market-data-info-popover-sheet"));
      expect(screen.queryByRole("region", { name: "About market data" })).not.toBeInTheDocument();
    } finally {
      document.body.style.setProperty("--phone-max-width", originalBreakpoint);
      Object.defineProperty(window, "innerWidth", { configurable: true, writable: true, value: originalWidth });
      act(() => window.dispatchEvent(new Event("resize")));
    }
  });

  it("renders nothing until the server confirms enablement", () => {
    const { container } = render(<MarketDataPanel {...props()} data={undefined} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("handles partial data without hiding successful metrics", () => {
    const partial = data();
    partial.estimatedHashpriceUsdPerPhDay = undefined;
    render(<MarketDataPanel {...props()} data={partial} />);
    expect(screen.getByText("$100,000.25")).toBeInTheDocument();
    expect(screen.getByText("Unavailable")).toBeInTheDocument();
    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("labels retained values stale and supports retry after a transport error", () => {
    const p = props();
    render(<MarketDataPanel {...p} error />);
    expect(screen.getAllByText(/Stale ·/)).toHaveLength(3);
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(p.onRetry).toHaveBeenCalledOnce();
  });

  it("marks provider failures and aged samples stale independently", () => {
    const p = props();
    p.data.bitcoinPriceUsd = metric(100000.25, "Coinbase", 0, true);
    p.data.networkHashrateHs = metric(1e21, "mempool.space", 3);
    render(<MarketDataPanel {...p} />);
    expect(screen.getAllByText(/Stale ·/)).toHaveLength(2);
    expect(screen.getByText("3 min ago")).toBeInTheDocument();
  });

  it("uses the server refresh cadence when determining age", () => {
    const p = props();
    p.data.refreshIntervalSeconds = 900;
    p.data.bitcoinPriceUsd = metric(100000.25, "Coinbase", 10);
    render(<MarketDataPanel {...p} />);
    expect(screen.queryByText(/Stale ·/)).not.toBeInTheDocument();
  });

  it("hides expired, nonfinite, and nonpositive samples", () => {
    const p = props();
    p.data.bitcoinPriceUsd = metric(100000.25, "Coinbase", 60);
    p.data.networkHashrateHs = metric(Infinity, "mempool.space");
    p.data.estimatedHashpriceUsdPerPhDay = metric(0, "Coinbase + mempool.space");
    render(<MarketDataPanel {...p} />);
    expect(screen.getAllByText("Unavailable")).toHaveLength(3);
    expect(screen.queryByText("$100,000.25")).not.toBeInTheDocument();
  });

  it("renders nothing when the instance has market data turned off", () => {
    const { container } = render(<MarketDataPanel {...props()} data={create(GetMarketDataResponseSchema)} />);
    expect(container).toBeEmptyDOMElement();
    expect(screen.queryByText("Bitcoin price")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "About market data" })).not.toBeInTheDocument();
  });
});
