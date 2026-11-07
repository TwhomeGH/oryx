//
// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
//
import React from 'react';
import ReactDOM from 'react-dom';
import axios from 'axios';
import 'bootstrap/dist/css/bootstrap.min.css';
import './index.css';
import './i18n';
import App from './App';

// Any successful response means the management service is reachable again: clear the offline
// banner. Failures are classified by useApiErrorHandler (network errors never unmount the page).
axios.interceptors.response.use(
  (res) => {
    window.dispatchEvent(new Event('oryx:server-reachable'));
    return res;
  },
  (err) => Promise.reject(err)
);

ReactDOM.render(
  <React.StrictMode>
      <App />
  </React.StrictMode>,
  document.getElementById('root')
);
