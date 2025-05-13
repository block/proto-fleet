import { RouterProvider } from "react-router-dom";

import { createRouter } from "./router";
import { MinerHostingProvider } from "@/protoOS/contexts/MinerHostingContext";
import { AuthProvider } from "@/protoOS/features/auth/contexts/AuthContext";
import { PreferencesProvider } from "@/shared/features/preferences/PreferencesContext";

import "@/shared/styles/index.css";

const router = createRouter();

const Main = () => {
  return (
    <MinerHostingProvider>
      <AuthProvider>
        <PreferencesProvider>
          <RouterProvider router={router} />
        </PreferencesProvider>
      </AuthProvider>
    </MinerHostingProvider>
  );
};

export default Main;
