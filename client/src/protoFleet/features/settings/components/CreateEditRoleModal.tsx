import { useCallback, useMemo, useState } from "react";
import clsx from "clsx";

import { type RoleItem, useRoleManagement } from "@/protoFleet/api/useRoleManagement";
import {
  type DependencyGaps,
  type PermissionGroup,
  usePermissionCatalog,
} from "@/protoFleet/features/settings/utils/permissionCatalog";
import { Alert, ChevronDown } from "@/shared/assets/icons";
import Button, { variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Checkbox from "@/shared/components/Checkbox";
import Input from "@/shared/components/Input";
import Modal, { sizes } from "@/shared/components/Modal";
import { pushToast, STATUSES } from "@/shared/features/toaster";

interface CreateEditRoleModalProps {
  open?: boolean;
  /** When supplied the modal edits this role; otherwise it creates a new one. */
  role?: RoleItem | null;
  onDismiss: () => void;
  onSuccess: () => void;
}

const DESCRIPTION_MAX_LENGTH = 1024;

// Groups start collapsed so the catalog reads as a compact list. When editing,
// groups that already grant something open by default so current access is
// visible at a glance.
const collapsedFor = (permissions: string[], groups: PermissionGroup[]): Set<string> => {
  const collapsed = new Set<string>();
  groups.forEach((group) => {
    const anySelected = group.entries.some((entry) => permissions.includes(entry.key));
    if (!anySelected) collapsed.add(group.resource);
  });
  return collapsed;
};

const permissionTestId = (key: string) => `role-permission-${key.replace(/[^a-z0-9]+/gi, "-").toLowerCase()}`;

const CreateEditRoleModal = ({ open, role, onDismiss, onSuccess }: CreateEditRoleModalProps) => {
  const isVisible = open ?? true;
  const isEdit = !!role;
  const nameLocked = !!role?.builtin;

  const { createRole, updateRole } = useRoleManagement();
  const {
    permissionGroups,
    withRequiredReads,
    lockedReadKeys,
    dependencyGaps,
    isLoading: catalogLoading,
    error: catalogError,
  } = usePermissionCatalog();

  if (catalogLoading || catalogError) {
    return (
      <Modal open={isVisible} onDismiss={onDismiss} size={sizes.standard} title={isEdit ? "Edit role" : "Create role"}>
        {catalogError ? (
          <Callout intent="danger" prefixIcon={<Alert />} title={catalogError} />
        ) : (
          <div className="py-10 text-center text-text-primary-50">Loading permissions...</div>
        )}
      </Modal>
    );
  }

  return (
    <CreateEditRoleModalForm
      isVisible={isVisible}
      isEdit={isEdit}
      nameLocked={nameLocked}
      role={role ?? null}
      permissionGroups={permissionGroups}
      withRequiredReads={withRequiredReads}
      lockedReadKeys={lockedReadKeys}
      dependencyGaps={dependencyGaps}
      createRole={createRole}
      updateRole={updateRole}
      onDismiss={onDismiss}
      onSuccess={onSuccess}
    />
  );
};

interface FormProps {
  isVisible: boolean;
  isEdit: boolean;
  nameLocked: boolean;
  role: RoleItem | null;
  permissionGroups: PermissionGroup[];
  withRequiredReads: (selected: Iterable<string>) => string[];
  lockedReadKeys: (selected: Iterable<string>) => Set<string>;
  dependencyGaps: (selected: Iterable<string>) => DependencyGaps;
  createRole: ReturnType<typeof useRoleManagement>["createRole"];
  updateRole: ReturnType<typeof useRoleManagement>["updateRole"];
  onDismiss: () => void;
  onSuccess: () => void;
}

const CreateEditRoleModalForm = ({
  isVisible,
  isEdit,
  nameLocked,
  role,
  permissionGroups,
  withRequiredReads,
  lockedReadKeys,
  dependencyGaps,
  createRole,
  updateRole,
  onDismiss,
  onSuccess,
}: FormProps) => {
  // Form state is seeded from `role` via useState defaults. Callers
  // remount the modal (key={role?.roleId ?? "create"}) when switching
  // between create/edit or between two different roles, so the seed
  // happens exactly once per open and stale state can't leak.
  const [name, setName] = useState(role?.name ?? "");
  const [description, setDescription] = useState(role?.description ?? "");
  // `explicit` is the user's literal selection — only keys they have actively
  // checked. The effective `selected` set (derived below) layers required
  // reads on top via withRequiredReads. Keeping derived reads out of state
  // means unchecking the last dependent action drops them automatically.
  // On edit we filter through visibleKeys so a catalog entry the UI doesn't
  // render (e.g. a key added server-side but not yet wired into the groups)
  // can't survive an unrelated save; standalone reads like fleet:read are
  // rendered as real checkboxes so they round-trip correctly.
  const visibleKeys = useMemo(
    () => new Set(permissionGroups.flatMap((g) => g.entries.map((e) => e.key))),
    [permissionGroups],
  );
  const [explicit, setExplicit] = useState<Set<string>>(
    () => new Set((role?.permissions ?? []).filter((key) => visibleKeys.has(key))),
  );
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [errorMsg, setErrorMsg] = useState("");
  const [query, setQuery] = useState("");
  const [collapsed, setCollapsed] = useState<Set<string>>(() =>
    collapsedFor(role?.permissions ?? [], permissionGroups),
  );

  const selected = useMemo(() => new Set(withRequiredReads(explicit)), [explicit, withRequiredReads]);

  const allResources = useMemo(() => permissionGroups.map((group) => group.resource), [permissionGroups]);

  const toggleKey = useCallback((key: string, checked: boolean) => {
    setErrorMsg("");
    setExplicit((prev) => {
      const next = new Set(prev);
      if (checked) next.add(key);
      else next.delete(key);
      return next;
    });
  }, []);

  const toggleGroup = useCallback((keys: string[], checked: boolean) => {
    setErrorMsg("");
    setExplicit((prev) => {
      const next = new Set(prev);
      if (checked) keys.forEach((key) => next.add(key));
      else keys.forEach((key) => next.delete(key));
      return next;
    });
  }, []);

  const toggleCollapse = useCallback((resource: string) => {
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(resource)) {
        next.delete(resource);
      } else {
        next.add(resource);
      }
      return next;
    });
  }, []);

  const handleSave = useCallback(() => {
    if (!name.trim()) {
      setErrorMsg("Role name is required");
      return;
    }
    if (selected.size === 0) {
      setErrorMsg("Select at least one permission");
      return;
    }

    setIsSubmitting(true);
    setErrorMsg("");
    const permissions = [...selected];
    const trimmedName = name.trim();
    const trimmedDescription = description.trim();

    const handlers = {
      onSuccess: () => {
        pushToast({
          message: isEdit ? `Role "${trimmedName}" updated` : `Role "${trimmedName}" created`,
          status: STATUSES.success,
        });
        onSuccess();
        onDismiss();
      },
      onError: (message: string) => setErrorMsg(message || "Failed to save role. Please try again."),
      onFinally: () => setIsSubmitting(false),
    };

    if (isEdit && role) {
      updateRole({ roleId: role.roleId, name: trimmedName, description: trimmedDescription, permissions, ...handlers });
    } else {
      createRole({ name: trimmedName, description: trimmedDescription, permissions, ...handlers });
    }
  }, [name, description, selected, isEdit, role, createRole, updateRole, onSuccess, onDismiss]);

  // Filter the catalog by the search query against group label, permission key,
  // and description. While a query is active the matching groups are forced
  // open and non-matching groups drop out, so collapse state is bypassed.
  const query_ = query.trim().toLowerCase();
  const searching = query_.length > 0;
  const renderedGroups = useMemo(() => {
    return permissionGroups
      .map((group) => {
        const labelMatch = group.label.toLowerCase().includes(query_);
        const entries =
          !searching || labelMatch
            ? group.entries
            : group.entries.filter(
                (entry) => entry.key.toLowerCase().includes(query_) || entry.description.toLowerCase().includes(query_),
              );
        return { group, entries };
      })
      .filter(({ entries }) => !searching || entries.length > 0);
  }, [query_, searching, permissionGroups]);

  const lockedReads = useMemo(() => lockedReadKeys(Array.from(selected)), [selected, lockedReadKeys]);

  // Permissions the current selection needs to be usable but doesn't grant
  // yet (e.g. Schedules can't run an action without the matching miner
  // permission). Hard requirements are offered as a one-click add; "choose at
  // least one" sets are shown as guidance only, since granting every member
  // would over-grant sensitive actions when just one is needed.
  const descriptionByKey = useMemo(() => {
    const map = new Map<string, string>();
    permissionGroups.forEach((group) => group.entries.forEach((entry) => map.set(entry.key, entry.description)));
    return map;
  }, [permissionGroups]);
  const describe = useCallback((key: string) => descriptionByKey.get(key) ?? key, [descriptionByKey]);
  const gaps = useMemo(() => dependencyGaps(Array.from(selected)), [selected, dependencyGaps]);
  const hasGaps = gaps.required.length > 0 || gaps.chooseOneOf.length > 0;
  const addRequiredDeps = useCallback(() => {
    setErrorMsg("");
    setExplicit((prev) => new Set([...prev, ...gaps.required]));
  }, [gaps.required]);

  return (
    <Modal
      open={isVisible}
      onDismiss={onDismiss}
      size={sizes.standard}
      title={isEdit ? "Edit role" : "Create role"}
      description={
        isEdit
          ? "Adjust the permissions this role grants. Members keep the role; their access updates immediately."
          : "Name the role and choose the permissions it grants. You can change these later."
      }
      buttons={[
        {
          text: isEdit ? "Save changes" : "Create role",
          onClick: handleSave,
          variant: variants.primary,
          loading: isSubmitting,
          dismissModalOnClick: false,
        },
      ]}
    >
      {errorMsg ? <Callout className="mb-6" intent="danger" prefixIcon={<Alert />} title={errorMsg} /> : null}

      <div className="mb-4">
        <Input
          id="role-name"
          label="Role name"
          initValue={name}
          onChange={(value) => setName(value)}
          disabled={nameLocked}
          autoFocus={!isEdit}
        />
      </div>

      <div className="mb-6">
        <Input
          id="role-description"
          label="Description"
          initValue={description}
          onChange={(value) => setDescription(value)}
          maxLength={DESCRIPTION_MAX_LENGTH}
        />
      </div>

      <div className="mb-3 flex items-center justify-between gap-4">
        <span className="text-emphasis-300 text-text-primary">Permissions</span>
        <div className="flex items-center gap-4">
          {!searching ? (
            <Button
              variant={variants.textOnly}
              text={collapsed.size === 0 ? "Collapse all" : "Expand all"}
              onClick={() => setCollapsed((prev) => (prev.size === 0 ? new Set(allResources) : new Set()))}
            />
          ) : null}
        </div>
      </div>

      <div className="mb-3">
        <Input
          id="permission-search"
          label="Search permissions"
          initValue={query}
          onChange={(value) => setQuery(value)}
          dismiss
          testId="role-permission-search"
        />
      </div>

      {hasGaps ? (
        <Callout
          className="mb-3"
          intent="warning"
          prefixIcon={<Alert />}
          title="This role needs more access to be usable"
          subtitle={
            <div className="flex flex-col gap-1">
              {gaps.required.length > 0 ? (
                <span>The permissions you selected also require: {gaps.required.map(describe).join(", ")}</span>
              ) : null}
              {gaps.chooseOneOf.map((group) => (
                <span key={group.join("|")}>
                  Choose at least one action to schedule: {group.map(describe).join(", ")}
                </span>
              ))}
            </div>
          }
          buttonText={gaps.required.length > 0 ? "Add required permissions" : undefined}
          buttonOnClick={gaps.required.length > 0 ? addRequiredDeps : undefined}
        />
      ) : null}

      {renderedGroups.length === 0 ? (
        <div className="py-10 text-center text-200 text-text-primary-50">No permissions match "{query.trim()}".</div>
      ) : (
        <div className="flex flex-col divide-y divide-border-5 border-y border-border-5">
          {renderedGroups.map(({ group, entries }) => {
            const groupKeys = entries.map((entry) => entry.key);
            const selectedInGroup = groupKeys.filter((key) => selected.has(key));
            const allSelected = groupKeys.length > 0 && selectedInGroup.length === groupKeys.length;
            const someSelected = selectedInGroup.length > 0 && !allSelected;
            const isOpen = searching || !collapsed.has(group.resource);

            return (
              <section key={group.resource}>
                <div className="flex items-center gap-3 py-3">
                  <Checkbox
                    className="shrink-0"
                    checked={allSelected}
                    partiallyChecked={someSelected}
                    onChange={(e) => toggleGroup(groupKeys, e.target.checked)}
                  />
                  <button
                    type="button"
                    className="flex flex-1 items-center justify-between gap-2 text-left"
                    onClick={() => toggleCollapse(group.resource)}
                    disabled={searching}
                    aria-expanded={isOpen}
                  >
                    <span className="text-emphasis-300 text-text-primary">{group.label}</span>
                    <span className="flex items-center gap-2">
                      <span className="text-300 text-text-primary-50">
                        {selectedInGroup.length}/{groupKeys.length}
                      </span>
                      {!searching ? (
                        <ChevronDown
                          className={clsx(
                            "h-4 w-4 text-text-primary transition-transform duration-200",
                            isOpen ? "rotate-180" : "",
                          )}
                        />
                      ) : null}
                    </span>
                  </button>
                </div>

                {isOpen ? (
                  <div className="flex flex-col">
                    {entries.map((entry, i) => {
                      const checked = selected.has(entry.key);
                      const isLast = i === entries.length - 1;
                      const isLocked = lockedReads.has(entry.key);
                      return (
                        <label
                          key={entry.key}
                          data-testid={permissionTestId(entry.key)}
                          className={clsx(
                            "flex items-center gap-3 py-2 pl-6 hover:bg-core-primary-2",
                            isLocked ? "cursor-not-allowed" : "cursor-pointer",
                            isLast && "pb-3",
                          )}
                        >
                          <Checkbox
                            className="shrink-0"
                            checked={checked}
                            disabled={isLocked}
                            onChange={(e) => toggleKey(entry.key, e.target.checked)}
                          />
                          <span className="text-300 text-text-primary">
                            {entry.description}
                            {isLocked ? <span className="text-text-primary-50"> (required)</span> : null}
                          </span>
                        </label>
                      );
                    })}
                  </div>
                ) : null}
              </section>
            );
          })}
        </div>
      )}
    </Modal>
  );
};

export default CreateEditRoleModal;
