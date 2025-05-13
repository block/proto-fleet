import { RouterProvider } from "react-router-dom";

import router from "./router";
import { AuthProvider } from "@/protoFleet/features/auth/contexts/AuthContext";
import { PreferencesProvider } from "@/shared/features/preferences/PreferencesContext";

import "@/shared/styles/index.css";

const Main = () => {
  return (
    <AuthProvider>
      <PreferencesProvider>
        <RouterProvider router={router} />
      </PreferencesProvider>
    </AuthProvider>
  );
};

export default Main;
