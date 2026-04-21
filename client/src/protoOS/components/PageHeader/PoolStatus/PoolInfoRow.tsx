import { ReactNode } from "react";

import Header from "@/shared/components/Header";
import Row from "@/shared/components/Row";

interface PoolInfoRowProps {
  hasDivider?: boolean;
  index?: number;
  suffixIcon?: ReactNode;
  url?: string;
}

const PoolInfoRow = ({ hasDivider, index, suffixIcon, url }: PoolInfoRowProps) => {
  return (
    <Row suffixIcon={suffixIcon} divider={hasDivider}>
      <Header
        title={`${index === 0 ? "Default Pool" : `Backup Pool #${index}`}`}
        titleSize="text-emphasis-300"
        subtitle={url}
        subtitleClassName="truncate"
        subtitleSize="text-200"
        className="bg-transparent! pl-4"
        compact
        showSubtitleTooltip
      />
    </Row>
  );
};

export default PoolInfoRow;
