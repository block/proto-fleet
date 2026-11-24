import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react";
import { spyOn, userEvent, within } from "storybook/test";

import { hostedAccountsClient } from "../api";
import ReportingSetup from "./ReportingSetup";
import { setupFixture } from "./reportingStoryFixtures";
import {
  HostReportingSetupSchema,
  ListHostReportingFactsResponseSchema,
  MutateHostReportingInputsResponseSchema,
  SetHostReportingAuthorityResponseSchema,
} from "@/protoFleet/api/generated/hosted/v1/reporting_pb";

const meta = {
  title: "Proto Fleet/Clients/Reporting setup",
  component: ReportingSetup,
  parameters: { layout: "padded" },
  args: { setup: setupFixture(), signal: new AbortController().signal, onChanged: () => {} },
  beforeEach: () => {
    const mocks = [
      spyOn(hostedAccountsClient, "listHostReportingFacts").mockResolvedValue(
        create(ListHostReportingFactsResponseSchema),
      ),
      spyOn(hostedAccountsClient, "mutateHostReportingInputs").mockResolvedValue(
        create(MutateHostReportingInputsResponseSchema),
      ),
      spyOn(hostedAccountsClient, "setHostReportingAuthority").mockResolvedValue(
        create(SetHostReportingAuthorityResponseSchema),
      ),
    ];
    return () => mocks.forEach((mock) => mock.mockRestore());
  },
} satisfies Meta<typeof ReportingSetup>;
export default meta;
type Story = StoryObj<typeof meta>;
export const PendingPrerequisite: Story = {};
export const ConfigurationEditor: Story = {
  play: async ({ canvasElement }) => {
    await userEvent.click(within(canvasElement).getByRole("button", { name: "Edit reporting configuration" }));
  },
};
/** An account with nothing saved: Fleet assumes the timezone and calendar year, and evidence is opt-in. */
export const AssumedDefaults: Story = {
  args: {
    setup: create(HostReportingSetupSchema, {
      accountId: "00000000-0000-4000-8000-000000000001",
      name: "Northwind Hosting",
      canCreate: true,
      input: { configuration: { timezone: "America/Chicago", reportingYearMonth: 1, reportingYearDay: 1 } },
    }),
  },
  play: ConfigurationEditor.play,
};
export const ImportFacts: Story = {
  play: async ({ canvasElement }) => {
    await userEvent.click(within(canvasElement).getByRole("button", { name: "Import facts" }));
  },
};
