import { createBrowserRouter } from "react-router-dom";

import Home from "./pages/Home";
import Logs from "./pages/MinerLogs";
import MiningPools from "./pages/Settings/MiningPools";
import Temperature from "./pages/Temperature";
import App from "@/protoOS/components/App";
import Auth from "@/protoOS/pages/Auth";
import Onboarding from "@/protoOS/pages/Onboarding";

export const routerConfig = [
  {
    path: "",
    element: (
      <App title="Home">
        <Home />
      </App>
    ),
  },
  {
    path: "auth",
    element: <Auth />,
  },
  {
    path: "temperature",
    element: (
      <App title="Temperature">
        <Temperature />
      </App>
    ),
  },
  {
    path: "home",
    element: (
      <App title="Home">
        <Home />
      </App>
    ),
  },
  {
    path: "logs",
    element: (
      <App title="Logs" fullScreen hideErrors>
        <Logs />
      </App>
    ),
  },
  {
    path: "onboarding",
    element: <Onboarding />,
  },
  {
    path: "settings/mining-pools",
    element: (
      <App title="Settings">
        <MiningPools />
      </App>
    ),
  },
];

const router = createBrowserRouter(routerConfig);

export default router;
