import { RouterProvider } from "react-router-dom";

import { createRouter } from "./router";
import { MinerHostingProvider } from "@/protoOS/contexts/MinerHostingContext";
import { AuthProvider } from "@/protoOS/features/auth/contexts/AuthContext";

import "@/shared/styles/index.css";

const router = createRouter();

const Main = () => {
  return (
    <MinerHostingProvider>
      <AuthProvider>
        <RouterProvider router={router} />
      </AuthProvider>
    </MinerHostingProvider>
  );
};

export default Main;
