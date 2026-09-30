import { useEffect, useId, useState } from "react";
import { Info } from "@/shared/assets/icons";
import Popover, { PopoverProvider, usePopover } from "@/shared/components/Popover";
import { positions } from "@/shared/constants";

function MarketDataInfo() {
  const [isOpen, setIsOpen] = useState(false);
  const contentId = useId();
  const { triggerRef, setPopoverRenderMode } = usePopover();

  useEffect(() => {
    setPopoverRenderMode("portal-scrolling");
  }, [setPopoverRenderMode]);

  return (
    <div ref={triggerRef}>
      <button
        type="button"
        aria-label="About market data"
        aria-expanded={isOpen}
        aria-controls={isOpen ? contentId : undefined}
        data-testid="market-data-info"
        className="flex h-8 w-8 items-center justify-center rounded-full text-text-primary-50 transition-colors hover:text-text-primary focus-visible:ring-2 focus-visible:ring-core-primary-20 focus-visible:outline-hidden"
        onClick={() => setIsOpen((current) => !current)}
      >
        <Info className="h-4 w-4" />
      </button>
      {isOpen ? (
        <Popover
          position={positions["bottom right"]}
          className="!rounded-2xl !bg-surface-elevated-base !p-4 !shadow-300 !backdrop-blur-none"
          closePopover={() => setIsOpen(false)}
          closeIgnoreSelectors={["[data-testid='market-data-info']"]}
          testId="market-data-info-popover"
        >
          <div
            id={contentId}
            role="region"
            aria-label="About market data"
            className="space-y-3 text-300 leading-6 text-text-primary-70"
          >
            <p>
              Hashprice estimates gross daily revenue per PH/s. Includes transaction fees; excludes pool fees and
              operating costs.
            </p>
            <p>Uses current difficulty and rewards from the last 144 blocks.</p>
            <p>Network hashrate averages 1,008 blocks (about 7 days).</p>
            <p>Checks every minute. Times show when data was retrieved, not published. Data may be delayed.</p>
          </div>
        </Popover>
      ) : null}
    </div>
  );
}

export default function MarketDataInfoPopover() {
  return (
    <PopoverProvider>
      <MarketDataInfo />
    </PopoverProvider>
  );
}
