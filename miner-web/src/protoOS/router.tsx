import { createBrowserRouter, Outlet, redirect } from "react-router-dom";

import App from "@/protoOS/components/App";
import {
  Efficiency,
  HashboardTemperature,
  Hashrate,
  KpiLayout,
  PowerUsage,
  Temperature,
} from "@/protoOS/features/kpis";
import {
  Cooling,
  General,
  Hardware,
  MiningPools,
} from "@/protoOS/features/settings";
import Auth from "@/protoOS/pages/Auth";
import Logs from "@/protoOS/pages/MinerLogs";
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
    path: "settings",
    // TODO: look into modifying App to use Outlet instead of children
    element: (
      <App title="Settings">
        <Outlet />
      </App>
    ),
    children: [
      {
        index: true,
        loader: () => redirect("general"),
      },
      {
        path: "general",
        element: <General />,
      },
      {
        path: "mining-pools",
        element: <MiningPools />,
      },
      {
        path: "hardware",
        element: <Hardware />,
      },
      {
        path: "cooling",
        element: <Cooling />,
      },
    ],
  },
];

const router = createBrowserRouter(routerConfig);

export default router;
