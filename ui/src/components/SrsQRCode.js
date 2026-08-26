//
// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
//
import React from "react";
import {QRCodeSVG} from "qrcode.react";

export default function SrsQRCode({url}) {
  if (!url) return <></>;
  return (
    <QRCodeSVG value={url} data-testid='qrCode' size={200} fgColor="#661111" />
  );
}
