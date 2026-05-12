import { useEffect } from "react";
import { ContentLayoutProps } from "@/protoOS/components/ContentLayout/types";
import { settingsRoutePrefetch } from "@/protoOS/router";
import { prefetchRoutes } from "@/shared/utils/prefetchRoutes";

const SettingsContentLayout = ({ children }: ContentLayoutProps) => {
  // Warm sibling /settings/* tab chunks at idle. The explicit return keeps
  // the cancel-on-unmount contract robust to a future block-body refactor.
  useEffect(() => {
    return prefetchRoutes(settingsRoutePrefetch);
  }, []);

  return (
    <div className="m-6 flex justify-center laptop:m-14">
      <div className="container mx-auto h-full w-full max-w-[640px] tablet:w-[584px] laptop:w-[608px] desktop:w-full">
        {children}
      </div>
    </div>
  );
};

export default SettingsContentLayout;
