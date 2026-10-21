//
// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
//
import React from "react";
import {Alert} from "react-bootstrap";
import {useTranslation} from "react-i18next";

// A one-line global banner for a dropped browser connection. It does not replace page content;
// it disappears by itself when the connection returns.
export function SrsOfflineBanner() {
  const {t} = useTranslation();
  const [online, setOnline] = React.useState(() => (typeof navigator === "undefined" ? true : navigator.onLine));

  React.useEffect(() => {
    const up = () => setOnline(true);
    const down = () => setOnline(false);
    window.addEventListener("online", up);
    window.addEventListener("offline", down);
    return () => {
      window.removeEventListener("online", up);
      window.removeEventListener("offline", down);
    };
  }, []);

  if (online) return null;
  return (
    <Alert variant="warning" className="rounded-0 mb-0 py-2 text-center small">
      {t("errs.offline")} — {t("errs.offlineHint")}
    </Alert>
  );
}