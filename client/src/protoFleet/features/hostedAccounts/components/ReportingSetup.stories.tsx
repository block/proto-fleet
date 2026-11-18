import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react";
import { spyOn, userEvent, within } from "storybook/test";

import { hostedAccountsClient } from "../api";
import ReportingSetup from "./ReportingSetup";
import { setupFixture } from "./reportingStoryFixtures";
import {
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
export const ImportFacts: Story = {
  play: async ({ canvasElement }) => {
    await userEvent.click(within(canvasElement).getByRole("button", { name: "Import facts" }));
  },
};
