import { useState } from "react";
import { MemoryRouter } from "react-router-dom";
import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react";

import { type AccountScope, AccountScopeContext } from "../AccountScope";
import AccountReports from "./AccountReports";
import { reportFixture, reportingStoryAccount, summaryFixture } from "./reportingStoryFixtures";
import { AccountAnalysisSchema } from "@/protoFleet/api/generated/hosted/v1/analysis_pb";
import { AccountSchema } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";

const savedAnalysis = create(AccountAnalysisSchema, {
  analysisId: "77777777-7777-4777-8777-777777777777",
  name: "October performance",
  kind: "miner_performance",
  start: { seconds: 1790812800n },
  end: { seconds: 1793491200n },
  createdAt: { seconds: 1793577600n },
  timezone: "UTC",
  limitations: ["Earlier history is unavailable."],
  tables: [
    { key: "performance", title: "Historical performance", columns: ["Hashrate"], rows: [{ cells: ["145.5 TH/s"] }] },
  ],
});
const ReportsPreview = ({
  complete = false,
  corrected = true,
  withAnalysis = false,
}: {
  complete?: boolean;
  corrected?: boolean;
  withAnalysis?: boolean;
}) => {
  const [scope] = useState<AccountScope>(() => ({
    account: create(AccountSchema, {
      accountId: reportingStoryAccount,
      name: "Northstar Mining",
      depth: 1,
      effectiveCapabilities: 1n,
    }),
    signal: new AbortController().signal,
    readOnly: true,
    refreshAccount: () => {},
    invalidateAccount: () => {},
    client: {
      exportAccountReport: async function* () {
        yield await Promise.reject<never>(new Error("Story preview has no downloadable artifact."));
      },
      listAccountReports: async () => ({ reports: [summaryFixture(complete, corrected)] }),
      listAccountAnalyses: async () => ({ analyses: withAnalysis ? [savedAnalysis] : [] }),
      getAccountAnalysis: async () => ({ analysis: savedAnalysis }),
      getAccountReport: async ({ reportId }: { reportId: string }) => ({
        report: reportFixture(complete, reportId === reportFixture().reportId && corrected),
      }),
    } as unknown as AccountScope["client"],
  }));
  return (
    <MemoryRouter>
      <AccountScopeContext.Provider value={scope}>
        <div className="min-h-screen bg-surface-base p-6 text-text-primary">
          <AccountReports />
        </div>
      </AccountScopeContext.Provider>
    </MemoryRouter>
  );
};
const meta = {
  title: "Proto Fleet/Clients/Reports",
  component: AccountReports,
  parameters: { layout: "fullscreen", withRouter: false },
} satisfies Meta<typeof AccountReports>;
export default meta;
type Story = StoryObj<typeof meta>;
export const IssuedCorrection: Story = { render: () => <ReportsPreview complete /> };
export const IncompleteReport: Story = { render: () => <ReportsPreview /> };
export const OriginalFirstPeriod: Story = { render: () => <ReportsPreview corrected={false} /> };
export const SharedLibrary: Story = { render: () => <ReportsPreview withAnalysis /> };
