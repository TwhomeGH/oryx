//
// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
//
import React from "react";
import {Button} from "react-bootstrap";
import {Moon, Sun} from "react-bootstrap-icons";

// Apply theme to the root element, Bootstrap 5.3 adapts all components.
export function applyTheme(theme) {
  document.documentElement.setAttribute('data-bs-theme', theme);
  localStorage.setItem('oryx-theme', theme);
}

// Light/dark toggle for the navbar.
export default function ThemeSwitch() {
  const [theme, setTheme] = React.useState(() =>
    typeof document !== 'undefined'
      ? (document.documentElement.getAttribute('data-bs-theme') || 'light')
      : 'light');

  return (
    <Button variant="outline-secondary" size="sm" className="me-2"
      title={theme === 'dark' ? 'Light mode' : 'Dark mode'}
      onClick={() => {
        const next = theme === 'dark' ? 'light' : 'dark';
        setTheme(next);
        applyTheme(next);
      }}>
      {theme === 'dark' ? <Sun/> : <Moon/>}
    </Button>
  );
}
