import { useCallback, useMemo, useState } from "react";
import clsx from "clsx";

import { type RoleItem, useRoleManagement } from "@/protoFleet/api/useRoleManagement";
import {
  lockedReadKeys,
  permissionGroups,
  withRequiredReads,
} from "@/protoFleet/features/settings/utils/permissionCatalog";
import { Alert, ChevronDown, Info, Lock } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Checkbox from "@/shared/components/Checkbox";
import Input from "@/shared/components/Input";
import Modal, { sizes } from "@/shared/components/Modal";
import Textarea from "@/shared/components/Textarea";
import { pushToast, STATUSES } from "@/shared/features/toaster";

interface CreateEditRoleModalProps {
  open?: boolean;
  /** When supplied the modal edits this role; otherwise it creates a new one. */
  role?: RoleItem | null;
  onDismiss: () => void;
  onSuccess: () => void;
}

const friendlyKeyAction = (key: string): string => {
  const action = (key.split(":")[1] ?? key).replace(/_/g, " ");
  return action.charAt(0).toUpperCase() + action.slice(1);
};

// Groups start collapsed so the 12-group catalog reads as a compact list. When
// editing, groups that already grant something open by default so current
// access is visible at a glance.
const collapsedFor = (permissions: string[]): Set<string> => {
  const collapsed = new Set<string>();
  permissionGroups.forEach((group) => {
    const anySelected = group.entries.some((entry) => permissions.includes(entry.key));
    if (!anySelected) collapsed.add(group.resource);
  });
  return collapsed;
};

const allResources = permissionGroups.map((group) => group.resource);

const CreateEditRoleModal = ({ open, role, onDismiss, onSuccess }: CreateEditRoleModalProps) => {
  const isVisible = open ?? true;
  const isEdit = !!role;
  const isBuiltin = !!role?.builtin;
  // Built-in names are stable server-side; only custom roles can be renamed.
  const nameLocked = isBuiltin;

  const { createRole, updateRole } = useRoleManagement();
  const [name, setName] = useState(role?.name ?? "");
  const [description, setDescription] = useState(role?.description ?? "");
  const [selected, setSelected] = useState<Set<string>>(new Set(role?.permissions ?? []));
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [errorMsg, setErrorMsg] = useState("");
  const [query, setQuery] = useState("");
  const [collapsed, setCollapsed] = useState<Set<string>>(() => collapsedFor(role?.permissions ?? []));

  // Re-seed form state whenever the modal opens or the target role changes.
  const [prevKey, setPrevKey] = useState<string | null>(null);
  const openKey = isVisible ? (role?.roleId ?? "new") : null;
  if (prevKey !== openKey) {
    setPrevKey(openKey);
    if (isVisible) {
      setName(role?.name ?? "");
      setDescription(role?.description ?? "");
      setSelected(new Set(role?.permissions ?? []));
      setIsSubmitting(false);
      setErrorMsg("");
      setQuery("");
      setCollapsed(collapsedFor(role?.permissions ?? []));
    }
  }

  const locked = useMemo(() => lockedReadKeys(selected), [selected]);

  const toggleKey = useCallback((key: string, checked: boolean) => {
    setErrorMsg("");
    setSelected((prev) => {
      if (checked) {
        return new Set(withRequiredReads([...prev, key]));
      }
      // Removing a key: drop it, then restore any reads still required by the
      // actions that remain selected. A read another action still depends on is
      // re-added here, so toggling a locked read off is a no-op (and the row is
      // marked with a lock icon to signal that).
      const next = new Set(prev);
      next.delete(key);
      return new Set(withRequiredReads(next));
    });
  }, []);

  const toggleGroup = useCallback((keys: string[], checked: boolean) => {
    setErrorMsg("");
    setSelected((prev) => {
      const next = new Set(prev);
      if (checked) {
        keys.forEach((key) => next.add(key));
        return new Set(withRequiredReads(next));
      }
      keys.forEach((key) => next.delete(key));
      // Keep cross-group reads that other selected actions still depend on.
      return new Set(withRequiredReads(next));
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

    const handlers = {
      onSuccess: () => {
        pushToast({
          message: isEdit ? `Role "${name.trim()}" updated` : `Role "${name.trim()}" created`,
          status: STATUSES.success,
        });
        onSuccess();
        onDismiss();
      },
      onError: (message: string) => setErrorMsg(message || "Failed to save role. Please try again."),
      onFinally: () => setIsSubmitting(false),
    };

    if (isEdit && role) {
      updateRole({ roleId: role.roleId, name: name.trim(), description, permissions, ...handlers });
    } else {
      createRole({ name: name.trim(), description, permissions, ...handlers });
    }
  }, [name, description, selected, isEdit, role, createRole, updateRole, onSuccess, onDismiss]);

  const selectedCount = selected.size;

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
  }, [query_, searching]);

  return (
    <Modal
      open={isVisible}
      onDismiss={onDismiss}
      size={sizes.large}
      title={isEdit ? `Edit ${role?.name}` : "Create role"}
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

      {isBuiltin ? (
        <Callout
          className="mb-6"
          intent="information"
          prefixIcon={<Info />}
          title="Built-in role"
          subtitle="This is a built-in role. You can adjust its permissions, but its name is fixed."
        />
      ) : null}

      <div className="mb-6 flex flex-col gap-4">
        <Input
          key={`name-${openKey}`}
          id="role-name"
          label="Role name"
          initValue={name}
          onChange={(value) => setName(value)}
          disabled={nameLocked}
          autoFocus={!isEdit}
        />
        <Textarea
          key={`desc-${openKey}`}
          id="role-description"
          label="Description"
          initValue={description}
          onChange={(value) => setDescription(value)}
          rows={2}
        />
      </div>

      <div className="mb-3 flex items-center justify-between gap-4">
        <span className="text-heading-100 text-text-primary">Permissions</span>
        <div className="flex items-center gap-4">
          {!searching ? (
            <button
              type="button"
              className="text-200 text-text-primary-50 hover:text-text-primary"
              onClick={() => setCollapsed((prev) => (prev.size === 0 ? new Set(allResources) : new Set()))}
            >
              {collapsed.size === 0 ? "Collapse all" : "Expand all"}
            </button>
          ) : null}
          <span className="text-200 text-text-primary-50">{selectedCount} selected</span>
        </div>
      </div>

      <div className="mb-3">
        <Input
          key={`search-${openKey}`}
          id="permission-search"
          label="Search permissions"
          initValue={query}
          onChange={(value) => setQuery(value)}
          dismiss
          compact
        />
      </div>

      {renderedGroups.length === 0 ? (
        <div className="rounded-xl border border-border-5 py-10 text-center text-200 text-text-primary-50">
          No permissions match “{query.trim()}”.
        </div>
      ) : (
        <div className="flex flex-col gap-3">
          {renderedGroups.map(({ group, entries }) => {
            const groupKeys = entries.map((entry) => entry.key);
            const selectedInGroup = groupKeys.filter((key) => selected.has(key));
            const allSelected = groupKeys.length > 0 && selectedInGroup.length === groupKeys.length;
            const someSelected = selectedInGroup.length > 0 && !allSelected;
            const isOpen = searching || !collapsed.has(group.resource);

            return (
              <section key={group.resource} className="overflow-hidden rounded-xl border border-border-5">
                <div className="flex items-center gap-3 px-4 py-3">
                  <Checkbox
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
                    <span className="flex items-center gap-2">
                      <span className="text-heading-100 text-text-primary">{group.label}</span>
                      <span className="text-200 text-text-primary-30">
                        {selectedInGroup.length}/{groupKeys.length}
                      </span>
                    </span>
                    {!searching ? (
                      <ChevronDown
                        className={clsx(
                          "h-4 w-4 text-text-primary-50 transition-transform duration-200",
                          isOpen ? "rotate-180" : "",
                        )}
                      />
                    ) : null}
                  </button>
                </div>

                {isOpen ? (
                  <div className="flex flex-col divide-y divide-border-5 border-t border-border-5">
                    {entries.map((entry) => {
                      const checked = selected.has(entry.key);
                      const isLocked = checked && locked.has(entry.key);
                      return (
                        <label
                          key={entry.key}
                          className="flex cursor-pointer items-start gap-3 px-4 py-3 hover:bg-core-primary-2"
                        >
                          <div className="pt-0.5">
                            <Checkbox checked={checked} onChange={(e) => toggleKey(entry.key, e.target.checked)} />
                          </div>
                          <div className="flex min-w-0 flex-1 flex-col">
                            <div className="flex flex-wrap items-center gap-2">
                              <span className="text-300 text-text-primary">{friendlyKeyAction(entry.key)}</span>
                              {isLocked ? (
                                <span className="flex items-center gap-1 rounded-full bg-core-primary-5 px-2 py-0.5 text-200 text-text-primary-50">
                                  <Lock className="h-3 w-3" />
                                  Required
                                </span>
                              ) : null}
                              <span className="font-mono text-200 text-text-primary-30">{entry.key}</span>
                            </div>
                            <span className="text-200 text-text-primary-50">{entry.description}</span>
                          </div>
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
