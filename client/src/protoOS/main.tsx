import { RouterProvider } from "react-router-dom";

import { createRouter } from "./router";
import { MinerHostingProvider } from "@/protoOS/contexts/MinerHostingContext";
import { SystemContextProvider } from "@/protoOS/contexts/SystemContext";
import { AuthProvider } from "@/protoOS/features/auth/contexts/AuthContext";
import { PreferencesProvider } from "@/shared/features/preferences/PreferencesContext";

import "@/shared/styles/index.css";

const router = createRouter();

const Main = () => {
  return (
    <MinerHostingProvider>
      <AuthProvider>
        <SystemContextProvider>
          <PreferencesProvider>
            <RouterProvider router={router} />
          </PreferencesProvider>
        </SystemContextProvider>
      </AuthProvider>
    </MinerHostingProvider>
  );
};

export default Main;
