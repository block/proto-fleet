import { useEffect } from "react";
import { ContentLayoutProps } from "@/protoOS/components/ContentLayout/types";
import { settingsRoutePrefetch } from "@/protoOS/router";
import { prefetchRoutes } from "@/shared/utils/prefetchRoutes";

const SettingsContentLayout = ({ children }: ContentLayoutProps) => {
  // Once the user is in /settings/*, the sibling tab chunks are one click
  // away; warm them at idle so tab switches resolve without a Suspense flash.
  useEffect(() => {
    prefetchRoutes(settingsRoutePrefetch);
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
