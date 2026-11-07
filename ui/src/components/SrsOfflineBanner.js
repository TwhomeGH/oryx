//
// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
//
import React from "react";
import {Alert} from "react-bootstrap";
import {useTranslation} from "react-i18next";

// A one-line global banner for a dropped connection. It never replaces page content: the view the
// user is reading stays as-is (with its last data) and only this strip appears. It covers both a
// browser offline event and the management service being unreachable (an API call with no
// response); a successful API response clears the latter automatically.
export function SrsOfflineBanner() {
  const {t} = useTranslation();
  const [online, setOnline] = React.useState(() => (typeof navigator === "undefined" ? true : navigator.onLine));
  const [serverDown, setServerDown] = React.useState(false);

  React.useEffect(() => {
    const up = () => setOnline(true);
    const down = () => setOnline(false);
    const unreachable = () => setServerDown(true);
    const reachable = () => setServerDown(false);
    window.addEventListener("online", up);
    window.addEventListener("offline", down);
    window.addEventListener("oryx:server-unreachable", unreachable);
    window.addEventListener("oryx:server-reachable", reachable);
    return () => {
      window.removeEventListener("online", up);
      window.removeEventListener("offline", down);
      window.removeEventListener("oryx:server-unreachable", unreachable);
      window.removeEventListener("oryx:server-reachable", reachable);
    };
  }, []);

  if (online && !serverDown) return null;
  return (
    <Alert variant="warning" className="rounded-0 mb-0 py-2 text-center small">
      {online ? t("errs.serverDown") : `${t("errs.offline")} — ${t("errs.offlineHint")}`}
    </Alert>
  );
}
