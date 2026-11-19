import type { ReactNode } from "react";
import ConnectedPoolsCard from "./ConnectedPoolsCard";
import PoolInfoRow from "./PoolInfoRow";
import type { PoolStatusEntry } from "./types";
import { getTexts } from "./utility";
import { isActivePool } from "@/protoOS/api/adapters/poolStatistics";
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Card, { cardType } from "@/shared/components/Card";
import Popover from "@/shared/components/Popover";
import { positions } from "@/shared/constants";

interface PoolInfoPopoverProps {
  details?: ReactNode;
  onClickViewPools: () => void;
  poolsInfo?: PoolStatusEntry[];
  showPoolStatusLink: boolean;
}

const PoolInfoPopover = ({ details, onClickViewPools, poolsInfo, showPoolStatusLink }: PoolInfoPopoverProps) => {
  const legacyPools = showPoolStatusLink ? undefined : poolsInfo;
  const connectedIndex = legacyPools?.findIndex(isActivePool) ?? -1;
  const isConnected = connectedIndex >= 0;
  const selectedIndex = isConnected ? connectedIndex : 0;
  const url = legacyPools?.[selectedIndex]?.url;
  const { title, subtitle, button } = getTexts({ isConnected, index: selectedIndex, url, showPoolStatusLink });

  return (
    <Popover
      title={title}
      subtitle={subtitle}
      buttons={[
        {
          text: button,
          onClick: onClickViewPools,
          variant: variants.secondary,
        },
      ]}
      position={positions["bottom left"]}
      testId="pool-info-popover"
    >
      {details}
      {url && isConnected ? <ConnectedPoolsCard pools={[{ id: selectedIndex, index: selectedIndex, url }]} /> : null}
      {url && !isConnected ? (
        <Card title="Not connected" type={cardType.warning}>
          {legacyPools?.map((pool, index) => (
            <PoolInfoRow
              key={pool.priority}
              index={index}
              url={pool.url}
              suffixIcon={<Alert className="text-intent-critical-fill" />}
              hasDivider={index < legacyPools.length - 1}
            />
          ))}
        </Card>
      ) : null}
    </Popover>
  );
};

export default PoolInfoPopover;
