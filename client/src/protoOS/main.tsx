import { RouterProvider } from "react-router-dom";

import { createRouter } from "./router";
import { MinerHostingProvider } from "@/protoOS/contexts/MinerHostingContext";
import ContainerFeedMappingProvider from "@/protoOS/features/feedMapping/FeedMappingProvider";
import { useWorkerNameUpdateScope } from "@/protoOS/features/poolAssignment/workerNameUpdates";
import { Toaster } from "@/shared/features/toaster";

import "@/shared/styles/index.css";

const router = createRouter();

const DirectMinerApp = () => {
  useWorkerNameUpdateScope();
  return (
    <>
      <div className="fixed right-4 bottom-4 z-60 phone:right-2 phone:bottom-2">
        <Toaster />
      </div>
      <ContainerFeedMappingProvider>
        <RouterProvider router={router} />
      </ContainerFeedMappingProvider>
    </>
  );
};

const Main = () => {
  return (
    <MinerHostingProvider>
      <DirectMinerApp />
    </MinerHostingProvider>
  );
};

export default Main;
