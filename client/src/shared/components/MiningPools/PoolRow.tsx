import { ReactNode, useMemo } from "react";

import { PoolIndex, PoolInfo } from "./types";
import Button, { sizes, variants } from "@/shared/components/Button";
import Row from "@/shared/components/Row";

interface PoolRowProps {
  poolIndex: PoolIndex;
  onClick: () => void;
  pools: PoolInfo[];
  testId?: string;
  /** Override the auto-generated title. Without this prop, displays pool.name > username > URL > "—" */
  title?: string;
  /** Optional second subtitle line shown below the URL. */
  subtitleExtra?: string;
  /** Show priority number badge (1-indexed) */
  priorityNumber?: number;
  /** Prefix element to show before pool info (e.g., grip handle for drag) */
  prefixElement?: ReactNode;
  /** Suffix element to show after Update button (e.g., menu) */
  suffixElement?: ReactNode;
}

const PoolRow = ({
  poolIndex,
  onClick,
  pools,
  testId,
  title,
  subtitleExtra,
  priorityNumber,
  prefixElement,
  suffixElement,
}: PoolRowProps) => {
  const pool = pools[poolIndex];
  const poolName = pool?.name;
  const url = pool?.url;
  const username = pool?.username;

  // Display title priority: explicit title prop > pool name > username > URL > fallback
  const displayTitle = title || poolName || username || url || "—";

  // Subtitle: show URL when we have a title, pool name, or username to display as primary
  const displaySubtitle = useMemo(() => {
    if (title || poolName || username) {
      return url || "Not configured";
    }
    return null;
  }, [title, poolName, username, url]);

  const displaySubtitleExtra = useMemo(() => {
    if (!subtitleExtra || subtitleExtra === displayTitle) {
      return null;
    }
    return subtitleExtra;
  }, [displayTitle, subtitleExtra]);

  return (
    <Row className="flex items-center justify-between gap-3" testId="pool-row">
      <div className="flex min-w-0 items-center gap-3">
        {priorityNumber !== undefined && (
          <div className="flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-full bg-surface-5 text-xs font-medium text-text-primary">
            {priorityNumber}
          </div>
        )}
        {prefixElement}
        <div className="flex min-w-0 flex-col">
          <div className="truncate text-text-primary">{displayTitle}</div>
          {(displaySubtitle || displaySubtitleExtra) && (
            <div className="text-200 text-text-primary-70">
              {displaySubtitle && (
                <div className="truncate" data-testid={`pool-${poolIndex}-saved-url`}>
                  {displaySubtitle}
                </div>
              )}
              {displaySubtitleExtra && (
                <div className="truncate" data-testid={`pool-${poolIndex}-saved-username`}>
                  {displaySubtitleExtra}
                </div>
              )}
            </div>
          )}
        </div>
      </div>
      <div className="flex shrink-0 items-center gap-2">
        <Button variant={variants.secondary} size={sizes.compact} text="Update" onClick={onClick} testId={testId} />
        {suffixElement}
      </div>
    </Row>
  );
};

export default PoolRow;
