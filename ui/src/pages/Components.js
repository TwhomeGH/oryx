//
// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
//
import Container from "react-bootstrap/Container";
import React from "react";
import {Token} from "../utils";
import axios from "axios";
import {Row, Col, Card, Badge, Button} from "react-bootstrap";
import * as moment from 'moment';
import {SrsErrorBoundary} from "../components/SrsErrorBoundary";
import {useTranslation} from "react-i18next";
import {SrsEnvContext} from "../components/SrsEnvContext";

export default function Components() {
  return (
    <SrsErrorBoundary>
      <ComponentsImpl />
    </SrsErrorBoundary>
  );
}

function ComponentsImpl() {
  const [status, setStatus] = React.useState();
  const [caps, setCaps] = React.useState();
  const {t} = useTranslation();
  const env = React.useContext(SrsEnvContext)[0];

  React.useEffect(() => {
    const refreshMgmtStatus = () => {
      axios.post('/terraform/v1/mgmt/status', {
      }, {
        headers: Token.loadBearerHeader(),
      }).then(res => {
        const status = res.data.data;

        // Normally state.
        setStatus(status);

        console.log(`${moment().format()}: Status: Query ok, status=${JSON.stringify(status)}`);
      }).catch(e => {
        console.log('ignore any error during status', e);
      });
    };

    refreshMgmtStatus();
    const timer = setInterval(() => refreshMgmtStatus(), 10 * 1000);
    return () => clearInterval(timer);
  }, [setStatus, env]);

  // Probe the SRS core capabilities once on mount; manual refresh by button,
  // because each probe spawns several srs -t processes.
  const refreshCapabilities = React.useCallback(() => {
    axios.post('/terraform/v1/mgmt/srs/capabilities', {}, {
      headers: Token.loadBearerHeader(),
    }).then(res => {
      setCaps(res.data.data);
      console.log(`Capabilities: Query ok ${JSON.stringify(res.data.data)}`);
    }).catch(e => {
      console.log('ignore error during capabilities', e);
      setCaps(null);
    });
  }, []);

  React.useEffect(() => {
    refreshCapabilities();
  }, [refreshCapabilities]);

  return (
    <>
      <Container fluid>
        <Row>
          <Col xs lg={3}>
            <Card style={{ width: '18rem', marginTop: '16px' }}>
              <Card.Header>{t('coms.host')}</Card.Header>
              <Card.Body>
                <Card.Text as={Col}>
                  {t('coms.version')}: {status?.version} <br/>
                  {t('coms.stable')}: {status?.version}<br/>
                  {t('coms.latest')}: <a href={t('coms.versionLink')} target='_blank' rel='noreferrer'>{status?.version}</a>
                </Card.Text>
              </Card.Body>
            </Card>
          </Col>
          <Col xs lg={3}>
            <Card style={{ width: '18rem', marginTop: '16px' }}>
              <Card.Header>{t('coms.srsTitle')}</Card.Header>
              <Card.Body>
                <Card.Text as={Col}>
                  {t('coms.srsVersion')}: {caps?.version || caps?.versionError || '-'} <br/>
                  {(caps?.features || []).map(f => (
                    <React.Fragment key={f.name}>
                      <Badge bg={f.ok ? 'success' : 'danger'} style={{marginRight: '4px'}}>{f.name}</Badge>
                    </React.Fragment>
                  ))}
                  {!caps && <>-</>}
                  {!!caps?.features?.some(f => !f.ok) && (
                    <div style={{fontSize: '0.8em', marginTop: '4px'}}>
                      {caps.features.filter(f => !f.ok).map(f => `${f.name}: ${f.detail}`).join('; ')}
                    </div>
                  )}
                </Card.Text>
                <Button size="sm" variant="outline-primary" onClick={refreshCapabilities}>{t('coms.refresh')}</Button>
              </Card.Body>
            </Card>
          </Col>
        </Row>
      </Container>
    </>
  );
}

