import { createBrowserRouter, RouterProvider } from "react-router-dom";
import ReactDOM from "react-dom/client";

import Hardware from "pages/Hardware";
import Help from "pages/Help";
import Performance from "pages/Performance";
import Settings from "pages/Settings";

import App from "./App.tsx";

import "./index.css";

const router = createBrowserRouter([
  {
    path: "/",
    element: <App><Performance /></App>,
  },
  {
    path: "/performance",
    element: <App><Performance /></App>,
  },
  {
    path: "/hardware",
    element: <App><Hardware /></App>,
  },
  {
    path: "/settings",
    element: <App><Settings /></App>,
  },
  {
    path: "/help",
    element: <App><Help /></App>,
  },
]);

ReactDOM.createRoot(document.getElementById("root")!).render(<RouterProvider router={router} />);
