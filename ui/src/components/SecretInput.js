//
// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
//
import React from "react";
import {InputGroup, Form, Button} from "react-bootstrap";
import {Eye, EyeSlash} from "react-bootstrap-icons";

// A secret input, masked by default with an eye button to reveal,
// to avoid leaking keys or passwords during live debugging or screen sharing.
export function SecretInput({id, value, onChange}) {
  const [show, setShow] = React.useState(false);

  return (
    <InputGroup>
      <Form.Control type={show ? 'text' : 'password'} id={id} defaultValue={value}
        autoComplete="off"
        onChange={(e) => onChange(e.target.value)} />
      <Button variant="outline-secondary" title={show ? 'Hide' : 'Show'}
        onClick={(e) => {
          e.preventDefault();
          setShow(!show);
        }}>
        {show ? <EyeSlash/> : <Eye/>}
      </Button>
    </InputGroup>
  );
}
