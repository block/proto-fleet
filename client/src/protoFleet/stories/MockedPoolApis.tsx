import { type ReactNode, useEffect } from "react";
import { create } from "@bufbuild/protobuf";

import { fleetManagementClient, onboardingClient, poolsClient } from "@/protoFleet/api/clients";
import { GetMinerPoolAssignmentsResponseSchema } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { GetFleetOnboardingStatusResponseSchema } from "@/protoFleet/api/generated/onboarding/v1/onboarding_pb";
import {
  CreatePoolResponseSchema,
  DeletePoolResponseSchema,
  ListPoolsResponseSchema,
  type Pool,
  PoolSchema,
  UpdatePoolResponseSchema,
  ValidatePoolResponseSchema,
} from "@/protoFleet/api/generated/pools/v1/pools_pb";
import { createRefCountedStoryMock } from "@/shared/stories/createRefCountedStoryMock";

type MutableClient<T> = { -readonly [K in keyof T]: T[K] };

const mutablePoolsClient = poolsClient as MutableClient<typeof poolsClient>;
const mutableFleetManagementClient = fleetManagementClient as MutableClient<typeof fleetManagementClient>;
const mutableOnboardingClient = onboardingClient as MutableClient<typeof onboardingClient>;

const defaultPools = [
  create(PoolSchema, {
    poolId: 1n,
    poolName: "Client pool A1",
    url: "stratum+tcp://mine.ocean.xyz:3334",
    username: "mann23",
  }),
  create(PoolSchema, {
    poolId: 2n,
    poolName: "Client pool A2",
    url: "stratum+tcp://mine.ocean.xyz:3323",
    username: "mann25",
  }),
  create(PoolSchema, {
    poolId: 3n,
    poolName: "Client pool A3",
    url: "stratum+tcp://mine.ocean.xyz:3344",
    username: "mann27",
  }),
];

export const MockedPoolApis = ({ children }: { children: ReactNode }) => {
  useEffect(() => {
    return installMockedPoolApis();
  }, []);

  return <>{children}</>;
};

const installMockedPoolApis = createRefCountedStoryMock(() => {
  let nextPoolId = defaultPools.reduce((maxId, pool) => (pool.poolId > maxId ? pool.poolId : maxId), 0n) + 1n;
  let pools: Pool[] = [...defaultPools];
  const originalListPools = mutablePoolsClient.listPools;
  const originalCreatePool = mutablePoolsClient.createPool;
  const originalUpdatePool = mutablePoolsClient.updatePool;
  const originalDeletePool = mutablePoolsClient.deletePool;
  const originalValidatePool = mutablePoolsClient.validatePool;
  const originalGetMinerPoolAssignments = mutableFleetManagementClient.getMinerPoolAssignments;
  const originalGetFleetOnboardingStatus = mutableOnboardingClient.getFleetOnboardingStatus;

  mutablePoolsClient.listPools = async () =>
    create(ListPoolsResponseSchema, {
      pools,
    });

  mutablePoolsClient.createPool = async (request) => {
    const nextPool = create(PoolSchema, {
      poolId: nextPoolId,
      poolName: request.poolConfig?.poolName ?? "",
      url: request.poolConfig?.url ?? "",
      username: request.poolConfig?.username ?? "",
    });
    nextPoolId += 1n;
    pools = [...pools, nextPool];

    return create(CreatePoolResponseSchema, {
      pool: nextPool,
    });
  };

  mutablePoolsClient.updatePool = async (request) => {
    const nextPool = create(PoolSchema, {
      poolId: request.poolId,
      poolName: request.poolName,
      url: request.url,
      username: request.username,
    });
    pools = pools.map((pool) => (pool.poolId === request.poolId ? nextPool : pool));

    return create(UpdatePoolResponseSchema, {
      pool: nextPool,
    });
  };

  mutablePoolsClient.deletePool = async (request) => {
    pools = pools.filter((pool) => pool.poolId !== request.poolId);
    return create(DeletePoolResponseSchema, {});
  };

  mutablePoolsClient.validatePool = async () => create(ValidatePoolResponseSchema, {});

  mutableFleetManagementClient.getMinerPoolAssignments = async () =>
    create(GetMinerPoolAssignmentsResponseSchema, {
      pools: [],
    });

  mutableOnboardingClient.getFleetOnboardingStatus = async () => create(GetFleetOnboardingStatusResponseSchema, {});

  return () => {
    mutablePoolsClient.listPools = originalListPools;
    mutablePoolsClient.createPool = originalCreatePool;
    mutablePoolsClient.updatePool = originalUpdatePool;
    mutablePoolsClient.deletePool = originalDeletePool;
    mutablePoolsClient.validatePool = originalValidatePool;
    mutableFleetManagementClient.getMinerPoolAssignments = originalGetMinerPoolAssignments;
    mutableOnboardingClient.getFleetOnboardingStatus = originalGetFleetOnboardingStatus;
  };
});
