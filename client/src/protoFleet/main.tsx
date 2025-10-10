import { RouterProvider } from "react-router-dom";

import router from "./router";
import { AuthProvider } from "@/protoFleet/features/auth/contexts/AuthContext";

import "@/shared/styles/index.css";

const Main = () => {
  return (
    <AuthProvider>
      <RouterProvider router={router} />
    </AuthProvider>
  );
};

export default Main;
