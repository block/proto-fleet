import { useMemo } from "react";
import { Outlet, useMatches } from "react-router-dom";

import AppLayout from "@/protoFleet/components/AppLayout";
import { getRouteMetadata } from "@/protoFleet/routes";

const App = () => {
  const matches = useMatches();
  const currentPath = useMemo(() => {
    return matches[matches.length - 1]?.pathname || "/";
  }, [matches]);

  const metadata = useMemo(() => {
    return getRouteMetadata(currentPath);
  }, [currentPath]);

  return (
    <>
      {metadata.useAppLayout ? (
        <AppLayout title={metadata?.title || ""}>
          <Outlet />
        </AppLayout>
      ) : (
        <Outlet />
      )}
    </>
  );
};

export default App;
