import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import type { Meta, StoryObj } from "@storybook/react";
import { expect, fn, userEvent, within } from "storybook/test";
import MarketDataPanel from "./MarketDataPanel";
import {
  GetMarketDataResponseSchema,
  MarketMetricSchema,
} from "@/protoFleet/api/generated/marketdata/v1/marketdata_pb";

const now = Date.now();
const metric = (value: number, source: string, stale = false) =>
  create(MarketMetricSchema, {
    value,
    source,
    stale,
    retrievedAt: timestampFromDate(new Date(now - (stale ? 5 * 60_000 : 0))),
  });
const data = create(GetMarketDataResponseSchema, {
  enabled: true,
  bitcoinPriceUsd: metric(84192.32, "Coinbase"),
  estimatedHashpriceUsdPerPhDay: metric(40.2, "Coinbase + mempool.space"),
  networkHashrateHs: metric(966.31e18, "mempool.space"),
});

const meta = {
  title: "Proto Fleet/Dashboard/MarketData",
  component: MarketDataPanel,
  tags: ["autodocs"],
  args: { data, error: false, now, onRetry: fn() },
} satisfies Meta<typeof MarketDataPanel>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Current: Story = {};
export const InfoOpen: Story = {
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    const page = within(canvasElement.ownerDocument.body);
    await userEvent.click(canvas.getByRole("button", { name: "About market data" }));
    await expect(await page.findByRole("region", { name: "About market data" })).toBeVisible();
  },
};
export const CheckingEnablement: Story = {
  args: { data: undefined },
  parameters: { docs: { description: { story: "The section stays hidden until the server confirms enablement." } } },
};
export const Stale: Story = { args: { error: true } };
export const Partial: Story = {
  args: {
    data: create(GetMarketDataResponseSchema, {
      ...data,
      estimatedHashpriceUsdPerPhDay: undefined,
      networkHashrateHs: metric(966.31e18, "mempool.space", true),
    }),
  },
};
export const Unavailable: Story = { args: { data: create(GetMarketDataResponseSchema, { enabled: true }) } };
export const Disabled: Story = {
  args: { data: create(GetMarketDataResponseSchema) },
  parameters: { docs: { description: { story: "Disabled instances hide the entire market-data section." } } },
  play: async ({ canvasElement }) => {
    await expect(within(canvasElement).queryByRole("region", { name: "Market data" })).not.toBeInTheDocument();
  },
};
