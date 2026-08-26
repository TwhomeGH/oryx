//
// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
//
// The contact page is a markdown-driven notice board for the fork maintainer.
// Content is read from the data volume (containers/data/contact.md), so it can
// be updated without rebuilding the image. When absent, a bundled default is
// rendered instead.
import React from "react";
import {Container, Card, Spinner} from "react-bootstrap";
import axios from "axios";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import {Token} from "../utils";
import {useSrsLanguage} from "../components/LanguageSwitch";
import {useErrorHandler} from "react-error-boundary";

import defaultZh from "../resources/contact-default.md?raw";
import defaultEn from "../resources/contact-default.en.md?raw";

// Split a markdown document into sections at each `## ` heading, so that a
// partial translation can overlay the main content section by section.
function splitSections(md) {
  const lines = (md || '').split(/\r?\n/);
  const sections = [];
  let cur = [];
  for (const line of lines) {
    if (/^##\s/.test(line) && cur.length) {
      sections.push(cur.join('\n'));
      cur = [line];
    } else {
      cur.push(line);
    }
  }
  if (cur.length) sections.push(cur.join('\n'));
  return sections;
}

// Overlay translated sections onto the base document. The overlay file only
// needs the sections it translates; the rest inherits from the base.
function overlayMarkdown(base, overlay) {
  const b = splitSections(base);
  const o = splitSections(overlay);
  const out = b.map((s, i) => i < o.length ? o[i] : s);
  for (let i = b.length; i < o.length; i++) out.push(o[i]);
  return out.join('\n\n');
}

export default function Contact() {
  const language = useSrsLanguage();
  return <ContactImpl locale={language === 'zh' ? 'zh' : 'en'} />;
}

function ContactImpl({locale}) {
  const handleError = useErrorHandler();
  const [content, setContent] = React.useState();
  const [custom, setCustom] = React.useState(false);

  React.useEffect(() => {
    axios.post('/terraform/v1/mgmt/contact/query', {}, {
      headers: Token.loadBearerHeader(),
    }).then(res => {
      const data = res.data.data || {};
      const zh = data.zh?.exists ? data.zh.content : '';
      const en = data.en?.exists ? data.en.content : '';
      const defaults = locale === 'zh' ? defaultZh : defaultEn;

      let customContent = '';
      if (locale === 'en') {
        // English: en file is a partial overlay of the main (zh) file; missing
        // sections fall back to the main content.
        if (zh && en) customContent = overlayMarkdown(zh, en);
        else if (en) customContent = en;
        else if (zh) customContent = zh;
      } else {
        // Chinese: use the main file; fall back to en-only if that's all there is.
        customContent = zh || en;
      }

      if (customContent) {
        setContent(customContent);
        setCustom(true);
      } else {
        setContent(defaults);
      }
    }).catch(handleError);
  }, [locale, handleError]);

  return (
    <Container fluid>
      {custom && (
        <div className="text-muted mb-2" style={{fontSize: '0.85em'}}>
          {locale === 'zh'
            ? `当前内容来自数据卷中的 contact${locale === 'en' ? '.en' : ''}.md，可直接编辑更新。`
            : `Content is loaded from contact${locale === 'en' ? '.en' : ''}.md in your data volume.`}
        </div>
      )}
      <Card body>
        {!content ? (
          <div className="text-center py-5"><Spinner animation="border" variant="success" /></div>
        ) : (
          <ReactMarkdown remarkPlugins={[remarkGfm]}>{content}</ReactMarkdown>
        )}
      </Card>
    </Container>
  );
}
