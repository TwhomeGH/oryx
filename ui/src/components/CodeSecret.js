//
// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
//
import React from "react";
import {Eye, EyeSlash} from "react-bootstrap-icons";

// A read-only secret shown as <code>, masked by default with an eye button to
// reveal. Unlike SecretInput (an editable input), this is for displaying a
// value inline (e.g. in a usage list) while keeping it hidden during screen
// sharing or demos.
export function CodeSecret({value}) {
  const [show, setShow] = React.useState(false);

  return (
    <span className="d-inline-flex align-items-center gap-1">
      <code>{show ? value : '••••••••'}</code>
      <span role="button" title={show ? 'Hide' : 'Show'} className="text-muted"
        onClick={(e) => {
          e.preventDefault();
          setShow(!show);
        }}>
        {show ? <EyeSlash size={16}/> : <Eye size={16}/>}
      </span>
    </span>
  );
}
