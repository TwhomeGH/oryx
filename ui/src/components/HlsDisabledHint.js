//
// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
//
import React from 'react';
import { Alert } from 'react-bootstrap';
import axios from 'axios';
import { useTranslation } from 'react-i18next';
import { useErrorHandler } from 'react-error-boundary';

import { Token } from '../utils';

// Shows a hint when the HLS master switch is off, so HLS links/players on this page don't
// silently break. Renders nothing while HLS is enabled.
export default function HlsDisabledHint() {
  const { t } = useTranslation();
  const handleError = useErrorHandler();
  const [enabled, setEnabled] = React.useState(true);

  React.useEffect(() => {
    axios.post('/terraform/v1/mgmt/srs/hls/query', {
    }, {
      headers: Token.loadBearerHeader(),
    }).then(res => {
      setEnabled(res.data.data.enabled !== false);
      console.log(`HlsDisabledHint: query ok, enabled=${JSON.stringify(res.data.data)}`);
    }).catch(handleError);
  }, [handleError]);

  if (enabled) return null;
  return <Alert variant="warning" className="py-2 mb-2">{t('hls.disabledHint')}</Alert>;
}
