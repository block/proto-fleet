import { useCallback, useMemo, useState } from "react";
import clsx from "clsx";

import { type RoleItem, useRoleManagement } from "@/protoFleet/api/useRoleManagement";
import {
  permissionGroups,
  withRequiredReads,
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

// Groups start collapsed so the catalog reads as a compact list. When editing,
// groups that already grant something open by default so current access is
// visible at a glance.
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
  const nameLocked = !!role?.builtin;

  // Form state is seeded from `role` via useState defaults. Callers
  // remount the modal (key={role?.roleId ?? "create"}) when switching
  // between create/edit or between two different roles, so the seed
  // happens exactly once per open and stale state can't leak.
  const { createRole, updateRole } = useRoleManagement();
  const [name, setName] = useState(role?.name ?? "");
  const [selected, setSelected] = useState<Set<string>>(new Set(role?.permissions ?? []));
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [errorMsg, setErrorMsg] = useState("");
  const [query, setQuery] = useState("");
  const [collapsed, setCollapsed] = useState<Set<string>>(() => collapsedFor(role?.permissions ?? []));

  const toggleKey = useCallback((key: string, checked: boolean) => {
    setErrorMsg("");
    setSelected((prev) => {
      if (checked) {
        return new Set(withRequiredReads([...prev, key]));
      }
      // Removing a read key cascades: drop it plus any actions in the same
      // resource that depended on it, then recompute required reads for
      // whatever remains.
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
      updateRole({ roleId: role.roleId, name: name.trim(), description: "", permissions, ...handlers });
    } else {
      createRole({ name: name.trim(), description: "", permissions, ...handlers });
    }
  }, [name, selected, isEdit, role, createRole, updateRole, onSuccess, onDismiss]);

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

      <div className="mb-6">
        <Input
          id="role-name"
          label="Role name"
          initValue={name}
          onChange={(value) => setName(value)}
          disabled={nameLocked}
          autoFocus={!isEdit}
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
        />
      </div>

      {renderedGroups.length === 0 ? (
        <div className="py-10 text-center text-200 text-text-primary-50">
          No permissions match "{query.trim()}".
        </div>
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
                      return (
                        <label
                          key={entry.key}
                          className={clsx(
                            "flex cursor-pointer items-center gap-3 py-2 pl-6 hover:bg-core-primary-2",
                            isLast && "pb-3",
                          )}
                        >
                          <Checkbox checked={checked} onChange={(e) => toggleKey(entry.key, e.target.checked)} />
                          <span className="text-300 text-text-primary">{entry.description}</span>
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
