import { ComponentType, lazy, Suspense } from "react";
import { RouterProvider } from "react-router-dom";

import { createRouter } from "./router";
import { MinerHostingProvider } from "@/protoOS/contexts/MinerHostingContext";

import "@/shared/styles/index.css";

const router = createRouter();

// Conditionally import MinefieldButton only in development
// @ts-ignore - minefield is optional and only available in development
const MinefieldButton: ComponentType<{ minefieldUrl: string }> | (() => null) =
  import.meta.env.DEV
    ? lazy(() =>
        // @ts-ignore
        import("@proto-fleet/minefield/component").then((m) => ({
          default: m.MinefieldButton,
        })),
      )
    : () => null;

const Main = () => {
  const isDev = import.meta.env.DEV;

  // this is only defined if started with minefield
  const minefieldUrl = import.meta.env.VITE_MINEFIELD_URL as string | undefined;

  return (
    <>
      <MinerHostingProvider>
        <RouterProvider router={router} />
      </MinerHostingProvider>
      {isDev && minefieldUrl && (
        <Suspense fallback={null}>
          <MinefieldButton minefieldUrl={minefieldUrl} />
        </Suspense>
      )}
    </>
  );
};

export default Main;
