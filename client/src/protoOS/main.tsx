import { RouterProvider } from "react-router-dom";

import { createRouter } from "./router";
import { MinerHostingProvider } from "@/protoOS/contexts/MinerHostingContext";
import { SystemContextProvider } from "@/protoOS/contexts/SystemContext";
import { AuthProvider } from "@/protoOS/features/auth/contexts/AuthContext";

import "@/shared/styles/index.css";

const router = createRouter();

const Main = () => {
  return (
    <MinerHostingProvider>
      <AuthProvider>
        <SystemContextProvider>
          <RouterProvider router={router} />
        </SystemContextProvider>
      </AuthProvider>
    </MinerHostingProvider>
  );
};

export default Main;
