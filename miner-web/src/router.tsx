import { createBrowserRouter } from "react-router-dom";

import Hardware from "pages/Hardware";
import Home from "pages/Home";
import Onboarding from "pages/Onboarding";
import Cooling from "pages/Settings/Cooling";
import MiningPools from "pages/Settings/MiningPools";

import App from "./App.tsx";

const router = createBrowserRouter([
  {
    path: "/",
    element: <App title="Home"><Home /></App>,
  },
  {
    path: "/hardware",
    element: <App title="Hardware"><Hardware /></App>,
  },
  {
    path: "/home",
    element: <App title="Home"><Home /></App>,
  },
  {
    path: "/onboarding",
    element: <Onboarding />,
  },
  {
    path: "/settings/mining-pools",
    element: <App title="Settings"><MiningPools /></App>,
  },
  {
    path: "/settings/cooling",
    element: <App title="Settings"><Cooling /></App>,
  },
]);

export default router;
