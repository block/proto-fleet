import { useEffect } from "react";
import { useLocation } from "react-router-dom";
import clsx from "clsx";
import { PAGE_PADDING } from "./spacing";
import { ContentLayoutProps } from "@/protoOS/components/ContentLayout/types";
import { settingsRoutePrefetch } from "@/protoOS/routePrefetch";
import { useDeviceType } from "@/protoOS/store";
import { prefetchRoutes } from "@/shared/utils/prefetchRoutes";

const SettingsContentLayout = ({ children, callout }: ContentLayoutProps) => {
  const { pathname } = useLocation();
  const wide = useDeviceType() === "container" && /\/settings\/(hardware|mining-pools)\/?$/.test(pathname);
  // Warm sibling /settings/* tab chunks at idle.
  useEffect(() => {
    return prefetchRoutes(settingsRoutePrefetch);
  }, []);

  return (
    <div className={PAGE_PADDING}>
      <div className="mb-6 empty:hidden">{callout}</div>
      <div
        className={clsx(
          "container mx-auto h-full w-full",
          wide ? "max-w-[1280px]" : "max-w-[640px] tablet:w-[584px] laptop:w-[608px] desktop:w-full",
        )}
      >
        {children}
      </div>
    </div>
  );
};

export default SettingsContentLayout;
