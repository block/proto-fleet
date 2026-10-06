import { useCallback, useRef, useState } from "react";

import { type DeviceSet } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import { type BulkCreateRackError, type NewRackInput, useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import { type BulkRackPlacement } from "@/protoFleet/features/fleetManagement/components/RackSettingsModal";

import { pushToast, STATUSES } from "@/shared/features/toaster";

/**
 * Bulk sibling of useCreateRack: one CreateRacks call for the whole batch.
 *
 * The RPC is all-or-nothing, so the result is either every rack in `created` or
 * none of them plus the per-row reasons the create modal marks its preview with.
 * That is the whole reason this isn't a loop over useCreateRack — a mid-list
 * failure would leave the operator working out which half of a prefix run
 * exists.
 */
export interface BulkRackCreateResult {
  created: DeviceSet[];
  errors: BulkCreateRackError[];
}

export interface UseCreateRacksResult {
  createRacks: (racks: NewRackInput[], placement: BulkRackPlacement) => Promise<BulkRackCreateResult>;
  creating: boolean;
}

export function useCreateRacks(): UseCreateRacksResult {
  const { createRacks: dispatchCreateRacks } = useDeviceSets();
  const [creating, setCreating] = useState(false);
  // The `creating` state lags a render behind the click, so the ref is what
  // actually blocks a double-click from dispatching two batches.
  const creatingRef = useRef(false);

  const createRacks = useCallback(
    async (racks: NewRackInput[], placement: BulkRackPlacement): Promise<BulkRackCreateResult> => {
      if (creatingRef.current) return { created: [], errors: [] };
      creatingRef.current = true;
      setCreating(true);
      return new Promise<BulkRackCreateResult>((resolve) => {
        void dispatchCreateRacks({
          siteId: placement.siteId,
          buildingId: placement.buildingId,
          // Rows arrive fully formed from the modal: it owns the label
          // generation and the one geometry the batch shares, so there is
          // nothing to reshape here.
          racks,
          onSuccess: (created) => {
            pushToast({
              message: `${created.length} ${created.length === 1 ? "rack" : "racks"} created`,
              status: STATUSES.success,
            });
            resolve({ created, errors: [] });
          },
          // A label collision comes back per row and the modal renders it against
          // the offending preview lines, so the toast only carries the summary.
          onError: (message, errors) => {
            pushToast({ message: `Failed to create racks: ${message}`, status: STATUSES.error });
            resolve({ created: [], errors });
          },
          onFinally: () => {
            creatingRef.current = false;
            setCreating(false);
          },
        });
      });
    },
    [dispatchCreateRacks],
  );

  return { createRacks, creating };
}
