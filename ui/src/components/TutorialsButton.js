//
// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
//
import React from "react";
import {Card, Col, Row} from "react-bootstrap";
import logo from '../resources/logo.svg';
import * as Icon from 'react-bootstrap-icons';
import {Token} from "../utils";
import axios from "axios";
import Container from "react-bootstrap/Container";
import {useTranslation} from "react-i18next";
import {useSrsLanguage} from "./LanguageSwitch";

// The tutorial manifest is served by the platform from /data/tutorials.json (seeded from
// the image default on the first boot), so the deployer can add or adjust the list without
// rebuilding or restarting. The manifest is fetched once and cached in the module.
let manifestCache;
let manifestPromise;

function loadTutorials() {
  if (manifestCache) return Promise.resolve(manifestCache);
  if (manifestPromise) return manifestPromise;
  manifestPromise = axios.post('/terraform/v1/mgmt/tutorials', {}, {
    headers: Token.loadBearerHeader(),
  }).then(res => {
    manifestCache = res.data.data?.tutorials || {};
    return manifestCache;
  }).catch((e) => {
    manifestPromise = null;
    throw e;
  });
  return manifestPromise;
}

/**
 * Fetch the tutorials of a context (e.g. 'live', 'ssl', 'all') from the platform manifest,
 * filtered by the current language. The list lives on the server, see /data/tutorials.json.
 * @param key The context key of the tutorials.
 * @returns A state of tutorials.
 */
function useTutorials(key) {
  const language = useSrsLanguage();
  const [tutorials, setTutorials] = React.useState([]);

  React.useEffect(() => {
    let cancelled = false;
    loadTutorials().then((manifest) => {
      if (cancelled) return;
      const list = (manifest[key] || []).filter(e => {
        if (!e.langs || !e.langs.length) return true;
        if (e.langs.includes(language)) return true;
        // Preserve the legacy behavior: non-Chinese languages fall back to the English list.
        return language !== 'zh' && e.langs.includes('en');
      });
      setTutorials(list);
    }).catch(() => {
      // The manifest may be unavailable, show nothing instead of crashing the page.
      if (!cancelled) setTutorials([]);
    });
    return () => { cancelled = true; };
  }, [key, language]);

  return tutorials;
}

// A toast list for tutorials.
// Format large view/like counts into compact "12.3K" style.
function fmtCount(n) {
  if (n == null || n === '') return null;
  const value = Number(n);
  if (!Number.isFinite(value)) return null;
  if (value >= 1000000) return (value / 1000000).toFixed(1).replace(/\.0$/, '') + 'M';
  if (value >= 1000) return (value / 1000).toFixed(1).replace(/\.0$/, '') + 'K';
  return String(value);
}

function tutorialDisplayTitle(tutorial, fallbackTitle = 'Watch') {
  if (tutorial.title) return tutorial.title;
  if (fallbackTitle) return fallbackTitle;
  return tutorial.link || '';
}

function TutorialsToast({tutorials, onClose}) {
  const {t} = useTranslation();

  return (<>
    <Container fluid>
      <Row>
        {tutorials.map((tutorial, index) => {
          const displayTitle = tutorialDisplayTitle(tutorial, t('tutorials.watch'));
          return (
            <Col key={index} xs={12} sm={6} md={6} lg={4} xl={3} className="mb-3">
              <Card className="h-100">
                <Card.Body className="d-flex flex-column">
                  <div className="d-flex align-items-start mb-2">
                    <img src={logo} className="rounded me-2 flex-shrink-0" width={40} height={40} alt=''/>
                    <div className="me-auto">
                      <div className="fw-semibold text-truncate">{tutorial.media}</div>
                      <small className="text-muted">by {tutorial.author}</small>
                    </div>
                    {onClose &&
                      <button type="button" className="btn-close" aria-label="Close" onClick={onClose}/>
                    }
                  </div>
                  <a href={tutorial.link} target='_blank' rel='noreferrer'
                     title={tutorial.id || tutorial.link}
                     className="d-block mb-2 flex-grow-1 text-decoration-none">
                    {displayTitle}
                  </a>
                  <div className="d-flex align-items-center gap-3 text-muted small">
                    {tutorial.view != null && (
                      <span title={t('tutorials.view')}>
                        <Icon.Play className="me-1"/>{fmtCount(tutorial.view)}
                      </span>
                    )}
                    {tutorial.like != null && (
                      <span title={t('tutorials.like')}>
                        <Icon.HandThumbsUp className="me-1"/>{fmtCount(tutorial.like)}
                      </span>
                    )}
                    {tutorial.share != null && (
                      <span title={t('tutorials.share')}>
                        <Icon.Share className="me-1"/>{fmtCount(tutorial.share)}
                      </span>
                    )}
                  </div>
                </Card.Body>
              </Card>
            </Col>
          );
        })}
      </Row>
    </Container>
  </>);
}

// The tutorials button, the props tutorials is a array, create by useTutorials.
function TutorialsButton({tutorials, prefixLine}) {
  const [show, setShow] = React.useState(false);

  return (
    <>
      <div role='button' style={{display: 'inline-block'}}>
        <Icon.PatchQuestion onClick={() => setShow(!show)} />
      </div>
      {show && prefixLine && <p></p>}
      {show &&
        <TutorialsToast
          prefixLine={prefixLine}
          tutorials={tutorials}
          onClose={() => setShow(false)}
        />
      }
    </>
  );
}

export {useTutorials, TutorialsButton, TutorialsToast, fmtCount, tutorialDisplayTitle};
