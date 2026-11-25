import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";

import CreateEditRoleModal from "./CreateEditRoleModal";

import { authzClient } from "@/protoFleet/api/clients";
import {
  CreateCustomRoleResponseSchema,
  RoleSchema,
  UpdateCustomRoleResponseSchema,
} from "@/protoFleet/api/generated/authz/v1/authz_pb";
import { type RoleItem } from "@/protoFleet/api/useRoleManagement";
import {
  buildPermissionGroups,
  dependencyGaps,
  lockedReadKeys,
  requiredReadsFor,
  usePermissionCatalog,
  withRequiredReads,
} from "@/protoFleet/features/settings/utils/permissionCatalog";
import { Toaster } from "@/shared/features/toaster";

vi.mock("@/protoFleet/features/settings/utils/permissionCatalog", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/protoFleet/features/settings/utils/permissionCatalog")>();
  return { ...actual, usePermissionCatalog: vi.fn() };
});

const updateRoleMock = vi.spyOn(authzClient, "updateCustomRole");
const createRoleMock = vi.spyOn(authzClient, "createCustomRole");
const catalog = [
  {
    key: "instance:update",
    description: "See available server updates, change the release channel, and apply server upgrades.",
    resource: "instance",
  },
  {
    key: "account:manage",
    description: "Manage client accounts and delegation. Does not grant access to client data or pool overrides.",
    resource: "account",
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
    vi.clearAllMocks();
    const savedRole = create(RoleSchema, { roleId: role.roleId, name: role.name });
    updateRoleMock.mockResolvedValue(create(UpdateCustomRoleResponseSchema, { role: savedRole }));
    createRoleMock.mockResolvedValue(create(CreateCustomRoleResponseSchema, { role: savedRole }));
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

  it.each(catalog)("renders and preserves $key when saving an existing role", async ({ key, description }) => {
    render(
      <>
        <Toaster />
        <CreateEditRoleModal open role={{ ...role, permissions: [key] }} onDismiss={vi.fn()} onSuccess={vi.fn()} />
      </>,
    );

    expect(screen.getByTestId(`role-permission-${key.replace(":", "-")}`)).toHaveTextContent(description);

    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    expect(updateRoleMock).toHaveBeenCalledWith(
      expect.objectContaining({
        roleId: role.roleId,
        name: role.name,
        description: role.description,
        permissionKeys: [key],
      }),
    );
    await waitFor(() => expect(screen.getByText(`Role "${role.name}" updated`)).toBeVisible());
  });

  it.each(["create", "edit"])(
    "shows a denied %s save through the API hook and preserves edits for retry",
    async (mode) => {
      const save = mode === "edit" ? updateRoleMock : createRoleMock;
      save.mockRejectedValueOnce(
        new ConnectError(
          "cannot grant account:manage: caller does not hold this permission at org scope",
          Code.PermissionDenied,
        ),
      );
      const onDismiss = vi.fn();
      const onSuccess = vi.fn();
      render(
        <>
          <Toaster />
          <CreateEditRoleModal open role={mode === "edit" ? role : null} onDismiss={onDismiss} onSuccess={onSuccess} />
        </>,
      );
      if (mode === "create") fireEvent.change(screen.getByLabelText("Role name"), { target: { value: role.name } });
      fireEvent.click(screen.getByRole("button", { name: /Administration/ }));
      fireEvent.click(screen.getByTestId("role-permission-account-manage"));
      const saveButton = screen.getByRole("button", { name: mode === "edit" ? "Save changes" : "Create role" });
      fireEvent.click(saveButton);

      const toast = await screen.findByTestId("toast");
      expect(toast).toHaveTextContent(/^You don't have permission to save these changes\.$/);
      expect(screen.getByTestId("role-permission-account-manage").querySelector("input")).toBeChecked();
      expect(screen.getByLabelText("Role name")).toHaveValue(role.name);
      expect(saveButton).toBeEnabled();
      expect(onDismiss).not.toHaveBeenCalled();
      expect(onSuccess).not.toHaveBeenCalled();
      expect(
        screen.queryByText(`Role "${role.name}" ${mode === "edit" ? "updated" : "created"}`),
      ).not.toBeInTheDocument();

      fireEvent.click(saveButton);
      await vi.waitFor(() => expect(onSuccess).toHaveBeenCalledOnce());
      expect(onDismiss).toHaveBeenCalledOnce();
      await waitFor(() =>
        expect(screen.getByText(`Role "${role.name}" ${mode === "edit" ? "updated" : "created"}`)).toBeVisible(),
      );
    },
  );
});
