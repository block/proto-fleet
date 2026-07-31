import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import CreateEditRoleModal from "./CreateEditRoleModal";

import { type RoleItem, useRoleManagement } from "@/protoFleet/api/useRoleManagement";
import {
  buildPermissionGroups,
  dependencyGaps,
  lockedReadKeys,
  requiredReadsFor,
  usePermissionCatalog,
  withRequiredReads,
} from "@/protoFleet/features/settings/utils/permissionCatalog";

vi.mock("@/protoFleet/api/useRoleManagement");
vi.mock("@/protoFleet/features/settings/utils/permissionCatalog", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/protoFleet/features/settings/utils/permissionCatalog")>();
  return { ...actual, usePermissionCatalog: vi.fn() };
});
vi.mock("@/shared/features/toaster", () => ({
  pushToast: vi.fn(),
  STATUSES: { error: "error", success: "success" },
}));

const updateRoleMock = vi.fn();
const catalog = [
  {
    key: "instance:update",
    description: "See available server updates, change the release channel, and apply server upgrades.",
    resource: "instance",
  },
];

const role: RoleItem = {
  roleId: "release-manager",
  name: "Release manager",
  description: "Can manage instance releases",
  permissions: ["instance:update"],
  builtin: false,
  memberCount: 1,
  updatedAt: null,
};

describe("CreateEditRoleModal", () => {
  beforeEach(() => {
    updateRoleMock.mockReset();
    vi.mocked(useRoleManagement).mockReturnValue({
      listRoles: vi.fn(),
      createRole: vi.fn(),
      updateRole: updateRoleMock,
      deleteRole: vi.fn(),
    });
    vi.mocked(usePermissionCatalog).mockReturnValue({
      catalog,
      permissionGroups: buildPermissionGroups(catalog),
      isLoading: false,
      error: null,
      requiredReadsFor: (key) => requiredReadsFor(key, catalog),
      withRequiredReads: (selected) => withRequiredReads(selected, catalog),
      lockedReadKeys: (selected) => lockedReadKeys(selected, catalog),
      dependencyGaps: (selected) => dependencyGaps(selected, catalog),
    });
  });

  it("renders and preserves instance:update when saving an existing role", () => {
    render(<CreateEditRoleModal open role={role} onDismiss={vi.fn()} onSuccess={vi.fn()} />);

    expect(screen.getByText("Instance")).toBeInTheDocument();
    expect(screen.getByTestId("role-permission-instance-update")).toHaveTextContent(catalog[0].description);

    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    expect(updateRoleMock).toHaveBeenCalledWith(
      expect.objectContaining({
        roleId: role.roleId,
        name: role.name,
        description: role.description,
        permissions: ["instance:update"],
      }),
    );
  });
});
