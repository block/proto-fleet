import { useCallback, useEffect, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";

import { fleetManagementClient } from "@/protoFleet/api/clients";
import { DeviceIdentifierListSchema } from "@/protoFleet/api/generated/common/v1/device_selector_pb";
import { type SortConfig } from "@/protoFleet/api/generated/common/v1/sort_pb";
import {
  type DeviceSelector,
  DeviceSelectorSchema,
  type MinerNameConfig,
  MinerNameConfigSchema,
  NamePropertySchema,
  ReviewWorkerNamesRequestSchema,
  type ReviewWorkerNamesResponse,
  StringPropertySchema,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import {
  WorkerNameReview,
  type WorkerNameUpdateResult,
} from "@/protoFleet/features/poolConfigurations/WorkerNameReview";
import { useAuthErrors, useFleetStore } from "@/protoFleet/store";

const useUpdateWorkerNames = () => {
  const { handleAuthErrors } = useAuthErrors();
  const generation = useFleetStore((state) => state.auth.sessionGeneration);
  type Pending = {
    review: ReviewWorkerNamesResponse;
    password: string;
    resolve: (result: WorkerNameUpdateResult) => void;
    reject: (error: Error) => void;
  };
  const [pending, setPending] = useState<Pending>();
  const active = useRef<Pending | undefined>(undefined);
  const lifecycle = useRef(0);
  useEffect(
    () => () => {
      lifecycle.current += 1;
      active.current?.reject(new ConnectError("Worker name review closed.", Code.Canceled));
      active.current = undefined;
      setPending(undefined);
    },
    [generation],
  );

  const updateWorkerNames = useCallback(
    async (
      deviceSelector: DeviceSelector,
      nameConfig: MinerNameConfig,
      _userUsername: string,
      userPassword: string,
      sort?: SortConfig,
    ): Promise<WorkerNameUpdateResult> => {
      const requestLifecycle = lifecycle.current;
      const requestGeneration = useFleetStore.getState().auth.sessionGeneration;
      try {
        const review = await fleetManagementClient.reviewWorkerNames(
          create(ReviewWorkerNamesRequestSchema, { deviceSelector, nameConfig, sort: sort ? [sort] : [] }),
        );
        if (lifecycle.current !== requestLifecycle) throw new ConnectError("Worker name review closed.", Code.Canceled);
        if (useFleetStore.getState().auth.sessionGeneration !== requestGeneration)
          throw new ConnectError("Session changed. Review worker names again.", Code.Canceled);
        if (!review.reviews.length)
          return {
            updatedCount: 0,
            unchangedCount: review.unchangedCount,
            failedCount: review.failedCount,
          };
        if (active.current) throw new Error("Finish the current worker name review first.");
        return await new Promise<WorkerNameUpdateResult>((resolve, reject) => {
          active.current = { review, password: userPassword, resolve, reject };
          setPending(active.current);
        });
      } catch (err) {
        if (
          lifecycle.current === requestLifecycle &&
          useFleetStore.getState().auth.sessionGeneration === requestGeneration
        )
          handleAuthErrors({ error: err });
        throw err;
      }
    },
    [handleAuthErrors],
  );

  const updateSingleWorkerName = useCallback(
    async (deviceIdentifier: string, name: string, userUsername: string, userPassword: string) =>
      updateWorkerNames(
        create(DeviceSelectorSchema, {
          selectionType: {
            case: "includeDevices",
            value: create(DeviceIdentifierListSchema, { deviceIdentifiers: [deviceIdentifier] }),
          },
        }),
        create(MinerNameConfigSchema, {
          properties: [
            create(NamePropertySchema, {
              kind: {
                case: "stringValue",
                value: create(StringPropertySchema, { value: name }),
              },
            }),
          ],
          separator: "",
        }),
        userUsername,
        userPassword,
      ),
    [updateWorkerNames],
  );

  const workerNameReview = pending ? (
    <WorkerNameReview
      review={pending.review}
      password={pending.password}
      onDone={(result) => {
        if (active.current !== pending) return;
        pending.resolve(result);
        active.current = undefined;
        setPending(undefined);
      }}
      onCancel={() => {
        if (active.current !== pending) return;
        pending.reject(new ConnectError("Worker name update canceled.", Code.Canceled));
        active.current = undefined;
        setPending(undefined);
      }}
    />
  ) : null;
  return { updateWorkerNames, updateSingleWorkerName, workerNameReview };
};

export default useUpdateWorkerNames;
