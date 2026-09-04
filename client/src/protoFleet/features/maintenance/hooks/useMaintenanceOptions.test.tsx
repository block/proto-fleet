import { renderHook, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";

const listAssignees = vi.fn(async ({ onSuccess, onFinally }) => {
  onSuccess([{ userId: 5n, username: "alex", roleName: "Technician" }]);
  onFinally();
});
vi.mock("@/protoFleet/api/maintenance", () => ({ useMaintenanceApi: () => ({ listAssignees }) }));
vi.mock("@/protoFleet/api/SitesContext", () => ({
  useSitesContext: () => ({ sites: [{ site: { id: 8n, name: "Denver" } }], sitesError: null }),
}));
vi.mock("@/protoFleet/store", () => ({ useUsername: () => "alex" }));
const { useMaintenanceOptions } = await import("./useMaintenanceOptions");

it("combines sites and organization-scoped assignees and identifies the caller", async () => {
  const { result } = renderHook(() => useMaintenanceOptions());
  await waitFor(() => expect(result.current.loading).toBe(false));
  expect(result.current.sites).toEqual([{ id: "8", name: "Denver" }]);
  expect(result.current.currentAssignee?.id).toBe("5");
});
