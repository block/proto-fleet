import type { Meta, StoryObj } from "@storybook/react";
import { userEvent, within } from "storybook/test";

import ReportContent from "./ReportContent";
import { reportFixture } from "./reportingStoryFixtures";

const meta = {
  title: "Proto Fleet/Clients/Report content",
  component: ReportContent,
  parameters: { layout: "padded" },
} satisfies Meta<typeof ReportContent>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Complete: Story = { args: { report: reportFixture(true) } };
export const Incomplete: Story = { args: { report: reportFixture() } };
export const Events: Story = {
  args: {
    report: (() => {
      const report = reportFixture();
      const event = report.result!.interruptions[0];
      Object.assign(event, {
        siteId: 1n,
        containerId: 2n,
        orderId: "Northstar order A",
        summary:
          "Planned electrical maintenance required a switchgear inspection, replacement of worn contacts, and staged restart checks across the affected equipment.",
        rootCause:
          "The inspection found worn contacts. The maintenance team replaced the affected parts, verified insulation resistance, and completed restart checks before returning the equipment to service.",
        sourceRef: "Approved maintenance log / signed inspection worksheet / restart verification record",
      });
      report.result!.interruptions.push({
        ...event,
        id: "ongoing",
        summary: "Cooling interruption under investigation",
        open: true,
        end: undefined,
        observedThrough: event.end,
      });
      return report;
    })(),
  },
  play: async ({ canvasElement }) => {
    await userEvent.click(within(canvasElement).getByRole("button", { name: "Events" }));
  },
};
export const Evidence: Story = {
  args: {
    report: (() => {
      const report = reportFixture();
      Object.assign(report.result!.containers[0].metrics.metered_energy_kwh, {
        status: "provisional",
        causes: ["estimated_meter_energy", "uncertified_meter_source"],
        method: "interval_sum_with_reviewed_commercial_coverage_and_attributed_meter_consumption",
        sources: [
          "Finance meter register / October / independently reviewed allocation worksheet",
          "Estimated interval accepted provisionally while the signed meter validation is pending",
          `https://reports.example.test/evidence/${"source-record-".repeat(15)}`,
        ],
      });
      return report;
    })(),
  },
  play: async ({ canvasElement }) => {
    await userEvent.click(within(canvasElement).getByRole("button", { name: "Evidence" }));
  },
};
