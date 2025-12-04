import { vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import PoolsListComponent from ".";
import { PoolSchema } from "@/protoFleet/api/generated/pools/v1/pools_pb";

interface PoolsListArgs {
  title: string;
  subtitle: string;
  createNewLabel: string;
  poolNumber?: number;
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
    validatePool: vi.fn(),
    createPool: vi.fn(),
    updatePool: vi.fn(),
    deletePool: vi.fn(),
    validatePoolPending: false,
  }),
}));

export const PoolsList = ({ title, subtitle, createNewLabel, poolNumber }: PoolsListArgs) => {
  return (
    <PoolsListComponent
      title={title}
      subtitle={subtitle}
      onSelect={() => {}}
      createNewLabel={createNewLabel}
      poolNumber={poolNumber}
    />
  );
};

export default {
  title: "Proto Fleet/Action Bar/Settings widget/Pools modal/Pools list",
  args: {
    title: "Default pool",
    subtitle: "",
    createNewLabel: "Add pool",
    poolNumber: undefined,
  },
  argTypes: {
    title: {
      control: "text",
    },
    subtitle: {
      control: "text",
    },
    createNewLabel: {
      control: "text",
    },
    poolNumber: {
      control: "number",
    },
  },
};
