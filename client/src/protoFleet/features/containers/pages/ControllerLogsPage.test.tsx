import { MemoryRouter, Route, Routes, useNavigate } from "react-router-dom";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import ControllerLogsPage from "./ControllerLogsPage";
import {
  ControllerLogCoverage,
  ControllerLogSeverity,
  GetContainerResponseSchema,
  GetControllerLogsResponseSchema,
} from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { useFleetStore } from "@/protoFleet/store";

const client = vi.hoisted(() => ({
  getContainer: vi.fn(),
  getControllerLogs: vi.fn(),
  listControllerAuditEvents: vi.fn(),
}));
vi.mock("@/protoFleet/api/clients", () => ({ containersClient: client }));
vi.mock("@/protoFleet/hooks/useSyncScopeToEntity", async (importOriginal) => ({
  ...(await importOriginal<object>()),
  useSyncScopeToEntity: vi.fn(),
}));
vi.mock("@/protoFleet/api/SitesContext", () => ({ useSitesContext: () => ({ sites: [] }) }));

function logs(message: string) {
  return create(GetControllerLogsResponseSchema, {
    entries: [
      { time: timestampFromDate(new Date()), severity: ControllerLogSeverity.INFO, component: "plant", message },
    ],
    limit: 1000,
    coverage: ControllerLogCoverage.RETAINED,
    controllerId: "ct1",
    readAt: timestampFromDate(new Date()),
  });
}

function Navigate() {
  const navigate = useNavigate();
  return <button onClick={() => navigate("/containers/bravo/controller-logs")}>Open bravo</button>;
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={["/containers/alpha/controller-logs"]}>
      <Navigate />
      <Routes>
        <Route path="/containers/:identifier/controller-logs" element={<ControllerLogsPage />} />
      </Routes>
    </MemoryRouter>,
  );
}

const signalFor = (identifier: string) =>
  client.getControllerLogs.mock.calls.find(([request]) => request.deviceIdentifier === identifier)?.[1]
    ?.signal as AbortSignal;

describe("ControllerLogsPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    Element.prototype.scrollIntoView = vi.fn();
    useFleetStore.setState((state) => {
      state.auth.permissions = ["site:read", "fleet:read", "miner:read", "miner:download_logs"];
    });
    client.getContainer.mockImplementation(async ({ deviceIdentifier }) =>
      create(GetContainerResponseSchema, { container: { deviceIdentifier, name: deviceIdentifier } }),
    );
  });

  it("clears the previous container's records and reads the new container on navigation", async () => {
    let finishBravo: (value: unknown) => void = () => {};
    client.getControllerLogs.mockImplementation(({ deviceIdentifier }) =>
      deviceIdentifier === "alpha"
        ? Promise.resolve(logs("alpha entry"))
        : new Promise((resolve) => (finishBravo = resolve)),
    );
    renderPage();
    expect(await screen.findByText(/alpha entry/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Open bravo" }));

    await waitFor(() => expect(signalFor("bravo")).toBeDefined());
    expect(screen.queryByText(/alpha entry/)).not.toBeInTheDocument();
    await act(async () => finishBravo(logs("bravo entry")));
    expect(await screen.findByText(/bravo entry/)).toBeInTheDocument();
  });

  it("aborts the previous container's read on navigation", async () => {
    client.getControllerLogs.mockImplementation(({ deviceIdentifier }) =>
      deviceIdentifier === "alpha" ? new Promise(() => {}) : Promise.resolve(logs("bravo entry")),
    );
    renderPage();
    await waitFor(() => expect(signalFor("alpha")).toBeDefined());

    fireEvent.click(screen.getByRole("button", { name: "Open bravo" }));

    expect(await screen.findByText(/bravo entry/)).toBeInTheDocument();
    expect(signalFor("alpha").aborted).toBe(true);
  });

  it("offers the audit trail only with activity read", async () => {
    client.getControllerLogs.mockResolvedValue(logs("alpha entry"));
    const { unmount } = renderPage();
    expect(await screen.findByText(/alpha entry/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Audit trail" })).not.toBeInTheDocument();
    unmount();

    useFleetStore.setState((state) => {
      state.auth.permissions.push("activity:read");
    });
    renderPage();
    expect(await screen.findByRole("button", { name: "Audit trail" })).toBeInTheDocument();
  });
});
