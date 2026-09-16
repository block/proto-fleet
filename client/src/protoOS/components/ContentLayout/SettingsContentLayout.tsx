import { useEffect } from "react";
import { useLocation } from "react-router-dom";
import clsx from "clsx";
import { ContentLayoutProps } from "@/protoOS/components/ContentLayout/types";
import { settingsRoutePrefetch } from "@/protoOS/routePrefetch";
import { useDeviceType } from "@/protoOS/store";
import { prefetchRoutes } from "@/shared/utils/prefetchRoutes";

const SettingsContentLayout = ({ children }: ContentLayoutProps) => {
  const { pathname } = useLocation();
  const wide = useDeviceType() === "container" && /\/settings\/hardware\/?$/.test(pathname);
  // Warm sibling /settings/* tab chunks at idle.
  useEffect(() => {
    return prefetchRoutes(settingsRoutePrefetch);
  }, []);

  return (
    <div className="m-6 flex justify-center laptop:m-14">
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
