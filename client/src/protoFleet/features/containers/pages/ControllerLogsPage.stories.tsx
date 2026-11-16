import { MemoryRouter, Route, Routes } from "react-router-dom";
import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import type { Meta, StoryObj } from "@storybook/react";
import { spyOn, userEvent, within } from "storybook/test";
import ControllerLogsPage from "./ControllerLogsPage";
import { containersClient } from "@/protoFleet/api/clients";
import {
  ControllerLogCoverage,
  ControllerLogSeverity,
  GetContainerResponseSchema,
  GetControllerLogsResponseSchema,
  ListControllerAuditEventsResponseSchema,
} from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { SiteWithCountsSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { SitesContext } from "@/protoFleet/api/SitesContext";
import { useFleetStore } from "@/protoFleet/store";

type State = "retained" | "interrupted" | "upgrade" | "audit" | "truncated";

function ControllerLogsPreview(_props: { state: State }) {
  return (
    <MemoryRouter initialEntries={["/containers/ct1-north/controller-logs"]}>
      <SitesContext.Provider
        value={{
          sites: [create(SiteWithCountsSchema, { site: { id: 2n, name: "North", slug: "north" } })],
          sitesError: null,
          sitesLoaded: true,
          sitesSettled: true,
          sitesPermissionDenied: false,
          siteCatalogAccessGranted: true,
          refetchSites: () => {},
          registerSitesPoll: () => () => {},
        }}
      >
        <main className="min-h-screen bg-surface-base text-text-primary">
          <Routes>
            <Route path="/containers/:identifier/controller-logs" element={<ControllerLogsPage />} />
          </Routes>
        </main>
      </SitesContext.Provider>
    </MemoryRouter>
  );
}

const at = (minutesAgo: number) => timestampFromDate(new Date(Date.now() - minutesAgo * 60_000));

const logEntries = [
  { minutes: 42, severity: ControllerLogSeverity.INFO, component: "plant", message: "dry cooler 1 started" },
  { minutes: 37, severity: ControllerLogSeverity.INFO, component: "pdu", message: "tank 2 feeder closed" },
  {
    minutes: 31,
    severity: ControllerLogSeverity.WARN,
    component: "plant",
    message: "supply temperature 41.8 °C above target 40 °C",
  },
  {
    minutes: 24,
    severity: ControllerLogSeverity.INFO,
    component: "ha",
    message: "peer ct1-north-b healthy, lease held by ct1-north-a",
  },
  {
    minutes: 18,
    severity: ControllerLogSeverity.ERROR,
    component: "hashboard",
    message: "tank 2 module 5 slot 3: no response from board",
  },
  {
    minutes: 12,
    severity: ControllerLogSeverity.INFO,
    component: "mining",
    message: "group default: 1,188 of 1,200 boards hashing",
  },
  {
    minutes: 6,
    severity: ControllerLogSeverity.WARN,
    component: "pdu",
    message: "tank 1 phase B current 92% of breaker rating",
  },
  {
    minutes: 1,
    severity: ControllerLogSeverity.INFO,
    component: "plant",
    message: "supply temperature back within target",
  },
].map(({ minutes, ...entry }) => ({ ...entry, time: at(minutes) }));

const auditEvents = [
  {
    id: "9182",
    minutes: 3,
    actor: "jordan@fleet",
    source: "grpc:10.20.0.4:50051",
    node: "ct1-north-a",
    action: "SetMiningGroup",
    target: "default",
    result: "ok",
    detailsJson: '{"mining":true}',
  },
  {
    id: "9181",
    minutes: 55,
    actor: "frigate",
    source: "system",
    node: "ct1-north-a",
    action: "HAFailover",
    target: "ct1-north-a",
    result: "ok",
    detailsJson: '{"from":"ct1-north-b"}',
  },
  {
    id: "9180",
    minutes: 180,
    actor: "sam@bridge",
    source: "rest:10.20.0.9:443",
    node: "ct1-north-b",
    action: "SetBreaker",
    target: "tank-2-main",
    result: "error",
    error: "interlock: dry cooler running",
  },
  {
    id: "9179",
    minutes: 600,
    actor: "jordan@fleet",
    source: "grpc:10.20.0.4:50051",
    node: "ct1-north-b",
    action: "SetPowerTarget",
    target: "container",
    result: "ok",
    detailsJson: '{"watts":1650000}',
  },
].map(({ minutes, ...event }) => ({ ...event, time: at(minutes) }));

// Long controller records: messages and fields cut to the relay limits, and a
// read that held fewer entries than the limit to fit its size budget.
const truncatedLogEntries = logEntries.map((entry, index) =>
  index === 2 || index === 5
    ? {
        ...entry,
        message: `${entry.message}; sensor dump: ${"t1=41.8 t2=41.6 t3=41.9 ".repeat(4)}`,
        messageTruncated: true,
      }
    : entry,
);
const truncatedAuditEvents = auditEvents.map((event, index) =>
  index === 0
    ? { ...event, detailsJson: `{"mining":true,"boards":[${'"t1-m1-s1",'.repeat(12)}`, truncated: true }
    : event,
);

const meta = {
  title: "Proto Fleet/Containers/Controller logs page",
  component: ControllerLogsPreview,
  parameters: { layout: "fullscreen", withRouter: false },
  args: { state: "retained" },
  beforeEach: ({ args }) => {
    const permissions = useFleetStore.getState().auth.permissions;
    useFleetStore.setState((state) => {
      state.auth.permissions = ["site:read", "fleet:read", "miner:read", "miner:download_logs", "activity:read"];
    });
    const mocks = [
      spyOn(containersClient, "getContainer").mockResolvedValue(
        create(GetContainerResponseSchema, {
          container: { id: 7n, deviceIdentifier: "ct1-north", name: "CT1 North", site: { id: 2n, label: "North" } },
        }),
      ),
      spyOn(containersClient, "getControllerLogs").mockImplementation(async ({ controllerId }) => {
        if (args.state === "upgrade")
          throw new ConnectError(
            "Upgrade this container's Fleet Node to read controller logs and audit events.",
            Code.FailedPrecondition,
          );
        const interrupted = args.state === "interrupted";
        const truncated = args.state === "truncated";
        return create(GetControllerLogsResponseSchema, {
          entries: interrupted ? logEntries.slice(0, 5) : truncated ? truncatedLogEntries : logEntries,
          limit: 1000,
          coverage: interrupted
            ? ControllerLogCoverage.INTERRUPTED
            : truncated
              ? ControllerLogCoverage.LIMIT_REACHED
              : ControllerLogCoverage.RETAINED,
          interruption: interrupted ? "the stream failed: connection reset by Fleet Node" : "",
          controllerId: controllerId || "ct1-north-a",
          controllerIds: ["ct1-north-a", "ct1-north-b"],
          readAt: timestampFromDate(new Date()),
        });
      }),
      spyOn(containersClient, "listControllerAuditEvents").mockImplementation(async ({ controllerId }) =>
        create(ListControllerAuditEventsResponseSchema, {
          events: args.state === "truncated" ? truncatedAuditEvents : auditEvents,
          limit: 1000,
          limitReached: args.state === "truncated",
          controllerId: controllerId || "ct1-north-a",
          controllerIds: ["ct1-north-a", "ct1-north-b"],
          readAt: timestampFromDate(new Date()),
        }),
      ),
    ];
    return () => {
      mocks.forEach((mock) => mock.mockRestore());
      useFleetStore.setState((state) => {
        state.auth.permissions = permissions;
      });
    };
  },
} satisfies Meta<typeof ControllerLogsPreview>;
export default meta;
type Story = StoryObj<typeof meta>;

export const HighAvailabilityLogs: Story = {};
export const InterruptedRead: Story = { args: { state: "interrupted" } };
export const FleetNodeUpgradeRequired: Story = { args: { state: "upgrade" } };
export const AuditTrail: Story = {
  args: { state: "audit" },
  play: async ({ canvasElement }) => {
    await userEvent.click(await within(canvasElement).findByRole("button", { name: "Audit trail" }));
  },
};
/** Long records: shortened entries are marked, and the notice counts what fit in one read. */
export const TruncatedLogs: Story = { args: { state: "truncated" } };
export const LogFilters: Story = {
  play: async ({ canvasElement }) => {
    await userEvent.click(await within(canvasElement).findByRole("button", { name: "Severity" }));
  },
};
export const TruncatedAuditEvent: Story = {
  args: { state: "truncated" },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(await canvas.findByRole("button", { name: "Audit trail" }));
    const row = (await canvas.findByText("SetMiningGroup")).closest("tr");
    if (row) await userEvent.click(row);
  },
};
