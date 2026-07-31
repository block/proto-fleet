import { useCallback, useRef, useState } from "react";

import { type PerDeviceRackConflict } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import { type RackFormData } from "@/protoFleet/features/fleetManagement/components/ManageRackModal/types";

import { pushToast, STATUSES } from "@/shared/features/toaster";

/**
 * Creates a rack from the Rack Settings form, optionally seeding it with
 * miners, and reports the new rack's id.
 *
 * SaveRack-with-no-id is the one call that can land dimensions, zone,
 * placement and a seeded member set in a single transaction, so a failed seed
 * can't strand an empty rack. That makes it right for create — and only for
 * create. Every subsequent edit goes through the delta RPCs (UpdateDeviceSet
 * for settings, AssignDevicesToRack for members and slots), because SaveRack
 * replaces the rack's whole member set and would clobber concurrent changes.
 *
 * Both rack entry points (RacksPage's "Add rack" and the bulk
 * "Add to rack → New rack" flow) share this so the create semantics — the
 * toast, the in-flight guard, the site-strip confirmation — can't drift apart.
 */
export interface UseCreateRackResult {
  /**
   * Creates the rack and resolves with its id, or undefined when the create
   * did not happen (an error, or a site-strip conflict awaiting confirmation).
   * On a conflict the returned promise resolves undefined and `conflict`
   * becomes non-null; call `confirmConflict` to retry with the strip forced.
   */
  createRack: (formData: RackFormData, seededMinerIds?: string[]) => Promise<bigint | undefined>;
  creating: boolean;
  /**
   * Non-null while a site-strip confirmation is pending, carrying what the
   * warning dialog needs: how many seeded miners would be displaced, and the
   * label of the rack they'd move into.
   */
  conflict: { count: number; rackLabel: string } | null;
  confirmConflict: () => void;
  cancelConflict: () => void;
}

export function useCreateRack({
  // Receives the submitted form data alongside the new id: the caller opens
  // ManageRackModal on it, and the forced-retry path fires this from the
  // confirmation dialog, where the form data is no longer in scope.
  onCreated,
}: {
  onCreated: (rackId: bigint, formData: RackFormData) => void;
}): UseCreateRackResult {
  const { saveRack } = useDeviceSets();
  const [creating, setCreating] = useState(false);
  // The `creating` state lags a render behind the click, so the ref is what
  // actually blocks a double-click from dispatching two creates.
  const creatingRef = useRef(false);
  const [conflict, setConflict] = useState<{ count: number; rackLabel: string } | null>(null);
  // Holds the inputs for the forced retry while the confirmation is showing.
  const pendingRef = useRef<{ formData: RackFormData; seededMinerIds: string[] } | null>(null);

  const dispatch = useCallback(
    (formData: RackFormData, seededMinerIds: string[], force: boolean): Promise<bigint | undefined> => {
      if (creatingRef.current) return Promise.resolve(undefined);
      creatingRef.current = true;
      setCreating(true);
      return new Promise<bigint | undefined>((resolve) => {
        saveRack({
          label: formData.label,
          zone: formData.zone,
          rows: formData.rows,
          columns: formData.columns,
          orderIndex: formData.orderIndex,
          coolingType: formData.coolingType,
          deviceIdentifiers: seededMinerIds,
          // Slots are the operator's next step, in the manage modal. Seeded
          // miners land as members without a position.
          slotAssignments: [],
          // Left undefined when the operator chose no placement. A new rack has
          // nothing to unassign, and an explicit 0 would make the field present
          // — which SaveRack reads as placement intent and gates behind
          // site:manage, denying a rack:manage-only operator their own create.
          siteId: formData.siteId,
          buildingId: formData.buildingId,
          forceClearConflictingSite: force,
          onSuccess: (deviceSet) => {
            pushToast({ message: `Rack "${formData.label}" created`, status: STATUSES.success });
            setConflict(null);
            pendingRef.current = null;
            onCreated(deviceSet.id, formData);
            resolve(deviceSet.id);
          },
          // Seeded miners that currently have a site/building the new rack
          // lacks would be stripped. The server wrote nothing; hold the inputs
          // so the caller's confirmation can retry with force.
          onConflicts: (conflicts: PerDeviceRackConflict[]) => {
            pendingRef.current = { formData, seededMinerIds };
            setConflict({ count: conflicts.length, rackLabel: formData.label });
            resolve(undefined);
          },
          onError: (message) => {
            pushToast({ message: message || "Failed to create rack. Please try again.", status: STATUSES.error });
            resolve(undefined);
          },
          onFinally: () => {
            creatingRef.current = false;
            setCreating(false);
          },
        });
      });
    },
    [saveRack, onCreated],
  );

  const createRack = useCallback(
    (formData: RackFormData, seededMinerIds?: string[]) => dispatch(formData, seededMinerIds ?? [], false),
    [dispatch],
  );

  const confirmConflict = useCallback(() => {
    const pending = pendingRef.current;
    if (!pending) return;
    setConflict(null);
    void dispatch(pending.formData, pending.seededMinerIds, true);
  }, [dispatch]);

  const cancelConflict = useCallback(() => {
    pendingRef.current = null;
    setConflict(null);
  }, []);

  return { createRack, creating, conflict, confirmConflict, cancelConflict };
}
