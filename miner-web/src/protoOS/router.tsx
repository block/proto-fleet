import { createBrowserRouter, redirect } from "react-router-dom";

import OldHome from "./pages/Home";
import Logs from "./pages/MinerLogs";
import MiningPools from "./pages/Settings/MiningPools";
import OldTemperature from "./pages/Temperature";
import App from "@/protoOS/components/App";
import {
  Efficiency,
  HashboardTemperature,
  Hashrate,
  KpiLayout,
  PowerUsage,
  Temperature,
} from "@/protoOS/features/kpis";
import Auth from "@/protoOS/pages/Auth";
import Onboarding from "@/protoOS/pages/Onboarding";

export const routerConfig = [
  {
    path: "",
    element: (
      <App fullScreen title="Home">
        <KpiLayout />
      </App>
    ),
    children: [
      {
        index: true,
        loader: () => redirect("hashrate"),
      },
      {
        path: "hashrate",
        element: <Hashrate />,
      },
      {
        path: "efficiency",
        element: <Efficiency />,
      },
      {
        path: "power-usage",
        element: <PowerUsage />,
      },
      {
        path: "temperature",
        element: <Temperature />,
      },
    ],
  },
  {
    path: "temperature/:serial",
    element: <HashboardTemperature />,
  },
  {
    path: "auth",
    element: <Auth />,
  },
  {
    path: "old-temperature",
    element: (
      <App title="Temperature">
        <OldTemperature />
      </App>
    ),
  },
  {
    path: "old-home",
    element: (
      <App title="Home">
        <OldHome />
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
