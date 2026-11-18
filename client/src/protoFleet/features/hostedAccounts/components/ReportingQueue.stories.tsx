import { MemoryRouter } from "react-router-dom";
import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react";
import { spyOn, userEvent, within } from "storybook/test";

import { hostedAccountsClient } from "../api";
import ReportingQueue from "./ReportingQueue";
import {
  reportFixture,
  reportingStoryAccount,
  reportingStoryUser,
  setupFixture,
  summaryFixture,
} from "./reportingStoryFixtures";
import {
  AcknowledgeHostReportQueueResponseSchema,
  CreateHostReportRequestResponseSchema,
  GetHostReportingSetupResponseSchema,
  GetHostReportResponseSchema,
  ListHostReportingAccountsResponseSchema,
  ListHostReportQueueResponseSchema,
  ListHostReportRequestsResponseSchema,
  ListHostReportsResponseSchema,
  PrepareHostReportResponseSchema,
  PublishHostReportResponseSchema,
  RecordHostReportDeliveryResponseSchema,
  RetryHostReportQueueNotificationResponseSchema,
} from "@/protoFleet/api/generated/hosted/v1/reporting_pb";

const meta = {
  title: "Proto Fleet/Clients/Reporting queue",
  component: ReportingQueue,
  parameters: { layout: "fullscreen", withRouter: false },
  beforeEach: ({ parameters }) => {
    const setup = setupFixture();
    const report = reportFixture();
    report.status = parameters.reportState ?? "issued";
    const summary = summaryFixture();
    summary.status = report.status;
    const mocks = [
      spyOn(hostedAccountsClient, "listHostReportingAccounts").mockResolvedValue(
        create(ListHostReportingAccountsResponseSchema, {
          accounts: [
            {
              accountId: reportingStoryAccount,
              name: setup.name,
              canCreate: true,
              canAdminister: true,
            },
          ],
        }),
      ),
      spyOn(hostedAccountsClient, "getHostReportingSetup").mockResolvedValue(
        create(GetHostReportingSetupResponseSchema, { setup }),
      ),
      spyOn(hostedAccountsClient, "listHostReportQueue").mockResolvedValue(
        create(ListHostReportQueueResponseSchema, {
          items: [
            {
              queueId: "obligation",
              accountId: reportingStoryAccount,
              accountName: setup.name,
              kind: "monthly",
              period: report.period,
              ownerUserId: reportingStoryUser,
              ownerUsername: "Casey",
              reasons: ["delivery_failed", "incomplete_inputs"],
              deliveryStatus: "failed",
              notificationStatus: "pending",
              canAcknowledge: true,
              generation: 1n,
              report: { report: summary },
              deadlines: { contract: { date: "2026-11-20", status: "authoritative" } },
            },
          ],
        }),
      ),
      spyOn(hostedAccountsClient, "listHostReports").mockResolvedValue(
        create(ListHostReportsResponseSchema, {
          reports: [{ report: summary, canPublish: report.status === "draft" }],
        }),
      ),
      spyOn(hostedAccountsClient, "getHostReport").mockResolvedValue(
        create(GetHostReportResponseSchema, {
          report: {
            report,
            canPublish: report.status === "draft",
            deliveries: [
              {
                requestId: "attempt",
                status: "failed",
                channel: "Agreed client portal",
                failureCategory: "unavailable",
                sourceRef: "Delivery log / failed attempt",
                evidence: "The client portal upload failed. Operations owns the retry.",
              },
            ],
          },
        }),
      ),
      spyOn(hostedAccountsClient, "listHostReportRequests").mockResolvedValue(
        create(ListHostReportRequestsResponseSchema),
      ),
      spyOn(hostedAccountsClient, "prepareHostReport").mockResolvedValue(create(PrepareHostReportResponseSchema)),
      spyOn(hostedAccountsClient, "publishHostReport").mockResolvedValue(create(PublishHostReportResponseSchema)),
      spyOn(hostedAccountsClient, "recordHostReportDelivery").mockResolvedValue(
        create(RecordHostReportDeliveryResponseSchema),
      ),
      spyOn(hostedAccountsClient, "createHostReportRequest").mockResolvedValue(
        create(CreateHostReportRequestResponseSchema),
      ),
      spyOn(hostedAccountsClient, "acknowledgeHostReportQueue").mockResolvedValue(
        create(AcknowledgeHostReportQueueResponseSchema),
      ),
      spyOn(hostedAccountsClient, "retryHostReportQueueNotification").mockResolvedValue(
        create(RetryHostReportQueueNotificationResponseSchema),
      ),
      spyOn(hostedAccountsClient, "exportHostReport").mockImplementation(async function* () {
        yield await Promise.reject<never>(new Error("Story preview has no downloadable artifact."));
      }),
    ];
    return () => mocks.forEach((mock) => mock.mockRestore());
  },
  render: () => (
    <MemoryRouter
      initialEntries={[
        `/accounts/reports?account=${reportingStoryAccount}&queue=obligation&report=${reportFixture().reportId}`,
      ]}
    >
      <div className="min-h-screen bg-surface-base p-6 text-text-primary">
        <ReportingQueue />
      </div>
    </MemoryRouter>
  ),
} satisfies Meta<typeof ReportingQueue>;
export default meta;
type Story = StoryObj<typeof meta>;
export const DeliveryFailure: Story = {};
export const Draft: Story = { parameters: { reportState: "draft" } };
export const PublishDraft: Story = {
  parameters: { reportState: "draft" },
  play: async ({ canvasElement }) => {
    await userEvent.click(await within(canvasElement).findByRole("button", { name: "Publish report" }));
  },
};
