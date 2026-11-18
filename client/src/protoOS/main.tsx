import { RouterProvider } from "react-router-dom";

import { createRouter } from "./router";
import { MinerHostingProvider } from "@/protoOS/contexts/MinerHostingContext";
import ContainerFeedMappingProvider from "@/protoOS/features/feedMapping/FeedMappingProvider";

import "@/shared/styles/index.css";

const router = createRouter();

const Main = () => {
  return (
    <MinerHostingProvider>
      <ContainerFeedMappingProvider>
        <RouterProvider router={router} />
      </ContainerFeedMappingProvider>
    </MinerHostingProvider>
  );
};

export default Main;
