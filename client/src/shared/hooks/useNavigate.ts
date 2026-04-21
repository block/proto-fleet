import { useCallback, useMemo } from "react";
import { To, useLocation, useNavigate as useReactNavigate } from "react-router-dom";

const useNavigate = () => {
  const reactNavigate = useReactNavigate();
  const location = useLocation();
  const { pathname } = useMemo(() => location, [location]);

  const navigate = useCallback(
    (path: string | number) => {
      reactNavigate(path as To, { state: { from: pathname } });
    },
    [pathname, reactNavigate],
  );

  return navigate;
};

export { useNavigate };
