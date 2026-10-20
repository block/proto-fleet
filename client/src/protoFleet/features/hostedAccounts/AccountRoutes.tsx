import { useState } from "react";
import { type Location, matchPath, Route, Routes, useLocation, useNavigate } from "react-router-dom";

import AccountDirectory from "./AccountDirectory";
import AccountWorkspace from "./AccountWorkspace";
import { can } from "./capabilities";
import ClientManagement from "./ClientManagement";
import { useHostedAccounts } from "./HostedAccountsContext";

const AccountRoutes = () => {
  const location = useLocation();
  const navigate = useNavigate();
  const directory = useHostedAccounts();
  const hostAdministration = directory?.accounts.some((account) => account.depth === 0 && can(account, 16n));
  const match = matchPath("/accounts/:accountId/*", location.pathname);
  const management = !!matchPath("/accounts/:accountId/manage/*", location.pathname);
  const root = directory?.accounts.some(
    (account) => account.accountId === match?.params.accountId && account.depth === 0,
  );
  const preview = hostAdministration && !directory?.loading && !!match && !management && !root;
  const [background, setBackground] = useState<Location>({
    pathname: "/accounts",
    search: "",
    hash: "",
    state: null,
    key: "client-preview-background",
  });
  if (!preview && !directory?.loading && background.key !== location.key) setBackground(location);
  const closePreview = () =>
    navigate(
      { pathname: background.pathname, search: background.search, hash: background.hash },
      { replace: true, state: background.state },
    );

  return (
    <>
      <div className="contents" inert={preview} aria-hidden={preview || undefined}>
        <Routes location={preview ? background : location}>
          <Route index element={<AccountDirectory />} />
          <Route path=":accountId/manage/:section?" element={<ClientManagement />} />
          <Route path=":accountId/assets/:kind/:assetId" element={<AccountWorkspace />} />
          <Route path=":accountId/:section?" element={<AccountWorkspace />} />
        </Routes>
      </div>
      {preview ? (
        <Routes>
          <Route path=":accountId/assets/:kind/:assetId" element={<AccountWorkspace onPreviewClose={closePreview} />} />
          <Route path=":accountId/:section?" element={<AccountWorkspace onPreviewClose={closePreview} />} />
        </Routes>
      ) : null}
    </>
  );
};

export default AccountRoutes;
