import { render, screen, within } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";

import ReportingActivity from "./ReportingActivity";
import { HostReportQueueItemSchema } from "@/protoFleet/api/generated/hosted/v1/reporting_pb";

it("shows recent activity first and reveals older events on request", async () => {
  const user = userEvent.setup();
  render(
    <ReportingActivity
      item={create(HostReportQueueItemSchema, {
        accountName: "Northstar",
        notificationStatus: "unrouted",
        deadlines: { contract: { reasonableEfforts: true, causes: ["reasonable_efforts_target", "missing_G5"] } },
        reasons: ["missing_G1", "missing_G1"],
        events: Array.from({ length: 12 }, (_, index) => ({
          kind: "attempted",
          reason: `Attempt ${index}`,
          createdAt: { seconds: BigInt(1791000000 + index) },
        })),
      })}
      onAcknowledge={vi.fn()}
      onRetry={vi.fn()}
    />,
  );
  await user.click(screen.getByText("Reporting activity"));
  expect(screen.getByText("No destination accepted the notification")).toBeVisible();
  expect(screen.getAllByText(/Reasonable.efforts target/i)).toHaveLength(1);
  const history = within(screen.getByRole("region", { name: "Activity history" }));
  expect(history.getAllByRole("row")).toHaveLength(11);
  expect(history.getAllByRole("row")[1]).toHaveTextContent("Attempt 11");
  expect(history.queryByText("Attempt 0")).not.toBeInTheDocument();
  await user.click(history.getByRole("button", { name: "Show older activity" }));
  expect(history.getByText("Attempt 0")).toBeVisible();
  expect(history.queryByRole("button", { name: "Show older activity" })).not.toBeInTheDocument();
  await user.click(screen.getByText("Data and workflow notes"));
  expect(screen.getAllByText("Contract definitions and calculation methods have not been approved.")).toHaveLength(1);
  expect(screen.queryByText("Missing G1")).not.toBeInTheDocument();
});
