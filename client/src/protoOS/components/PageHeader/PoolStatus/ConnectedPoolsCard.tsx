import PoolInfoRow from "./PoolInfoRow";
import { Success } from "@/shared/assets/icons";
import Card, { cardType } from "@/shared/components/Card";

interface ConnectedPool {
  id: string | number;
  name?: string;
  priority: number;
  url?: string;
}

export default function ConnectedPoolsCard({ pools }: { pools: ConnectedPool[] }) {
  return (
    <Card title="Connected" type={cardType.success}>
      {pools.map((pool, index) => (
        <PoolInfoRow
          key={pool.id}
          index={pool.priority}
          name={pool.name}
          url={pool.url}
          suffixIcon={<Success className="text-intent-success-fill" />}
          hasDivider={index < pools.length - 1}
        />
      ))}
    </Card>
  );
}
