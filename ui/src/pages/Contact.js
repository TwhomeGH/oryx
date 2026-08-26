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

export default function Contact() {
  const language = useSrsLanguage();
  return <ContactImpl locale={language === 'zh' ? 'zh' : 'en'} />;
}

function ContactImpl({locale}) {
  const handleError = useErrorHandler();
  const [content, setContent] = React.useState();
  const [custom, setCustom] = React.useState(false);

  React.useEffect(() => {
    axios.post('/terraform/v1/mgmt/contact/query', {
      locale,
    }, {
      headers: Token.loadBearerHeader(),
    }).then(res => {
      const data = res.data.data || {};
      if (data.exists && data.content) {
        setContent(data.content);
        setCustom(true);
      } else {
        setContent(locale === 'zh' ? defaultZh : defaultEn);
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
