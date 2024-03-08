import { createBrowserRouter } from "react-router-dom";

import Hardware from "pages/Hardware";
import Home from "pages/Home";
import Onboarding from "pages/Onboarding";
import Settings from "pages/Settings";

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
    path: "/settings",
    element: <App title="Settings"><Settings /></App>,
  },
]);

export default router;
