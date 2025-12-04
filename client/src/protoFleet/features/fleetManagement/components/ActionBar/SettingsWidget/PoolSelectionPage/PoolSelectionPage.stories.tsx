import { vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import PoolSelectionPageComponent from "./PoolSelectionPage";
import { PoolSchema } from "@/protoFleet/api/generated/pools/v1/pools_pb";

interface PoolSelectionPageArgs {
  numberOfMiners: number;
}

const mockPools = [
  create(PoolSchema, {
    poolId: BigInt(1),
    poolName: "Client pool A1",
    url: "stratum+tcp://mine.ocean.xyz:3334",
    username: "mann23",
  }),
  create(PoolSchema, {
    poolId: BigInt(2),
    poolName: "Client pool A2",
    url: "stratum+tcp://mine.ocean.xyz:3323",
    username: "mann25",
  }),
  create(PoolSchema, {
    poolId: BigInt(3),
    poolName: "Client pool A3",
    url: "stratum+tcp://mine.ocean.xyz:3344",
    username: "mann27",
  }),
];

vi.mock("@/protoFleet/api/usePools", () => ({
  default: () => ({
    pools: mockPools,
    miningPools: mockPools.map((pool) => ({
      poolId: pool.poolId.toString(),
      name: pool.poolName,
      poolUrl: pool.url,
      username: pool.username,
    })),
    validatePool: vi.fn(({ onSuccess }) => {
      onSuccess?.();
    }),
    createPool: vi.fn(),
    updatePool: vi.fn(),
    deletePool: vi.fn(),
    validatePoolPending: false,
  }),
}));

export const PoolSelectionPage = ({ numberOfMiners }: PoolSelectionPageArgs) => {
  const deviceIdentifiers = Array.from({ length: numberOfMiners }, (_, i) => `device-${i}`);

  return (
    <PoolSelectionPageComponent
      deviceIdentifiers={deviceIdentifiers}
      onAssignPools={async () => {}}
      onDismiss={() => {}}
    />
  );
};

export default {
  title: "Proto Fleet/Action Bar/Settings widget/Pool selection page",
  args: {
    numberOfMiners: 1,
  },
  argTypes: {
    numberOfMiners: {
      control: { type: "range", min: 1, max: 25, step: 1 },
    },
  },
};
