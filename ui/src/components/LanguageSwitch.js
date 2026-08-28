//
// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
//
import React from 'react';
import {Dropdown, Navbar} from "react-bootstrap";
import {Locale} from "../utils";
import {useLocation, useNavigate} from "react-router-dom";
import {useTranslation} from "react-i18next";
import {resources, locales as localeOptions} from "../localeLoader";

export default function LanguageSwitch() {
  const [locale, setLocale] = React.useState();
  const [locales, setLocales] = React.useState([]);
  const navigate = useNavigate();
  const location = useLocation();
  const {i18n} = useTranslation();

  React.useEffect(() => {
    const lang = location.pathname.split('/')[1];

    // Ignore if invalid language.
    if (!lang || !Object.keys(resources).includes(lang)) {
      return;
    }

    // Change to language in url.
    if (Locale.current() !== lang) {
      i18n.changeLanguage(lang);
      Locale.save({lang});
    }

    const previous = Locale.current();
    if (locale !== lang) {
      setLocale(Locale.current());
      console.log(`Detect language path=${location.pathname}, lang=${lang}, previous=${previous}, current=${Locale.current()}`);
    }
  }, [location, i18n, locale]);

  React.useEffect(() => {
    // Languages are dynamically discovered from resources/locale_*.json via
    // the localeLoader; adding a new file automatically adds it here.
    setLocales(localeOptions);
  }, []);

  const onChangeLocale = React.useCallback((lang) => {
    const jumpTo = location.pathname.replace(`${Locale.current()}`, lang);
    console.log(`Change language to ${lang}, jump to ${jumpTo}, search=${location.search}`);
    return navigate({pathname: jumpTo, search: location.search});
  }, [location, navigate]);

  return (
    <Dropdown>
      <Dropdown.Toggle variant='text'>
        {locales.map((e, index) => {
          if (e.code !== locale) return <React.Fragment key={index} />;
          return (
            <React.Fragment key={index}>
              <Navbar.Text>{e.name}</Navbar.Text>
            </React.Fragment>
          );
        })}
      </Dropdown.Toggle>
      <Dropdown.Menu>
        {locales.map((e, index) => {
          return (
            <Dropdown.Item
              key={index}
              onClick={() => onChangeLocale(e.code)}
            >
              <Navbar.Text>{e.name}</Navbar.Text>
            </Dropdown.Item>
          );
        })}
      </Dropdown.Menu>
    </Dropdown>
  );
}

function useSrsLanguage() {
  const [language, setLanguage] = React.useState(Locale.current());

  const ref = React.useRef({
    language: Locale.current(),
  });
  React.useEffect(() => {
    if (ref.current.language !== language) ref.current.language = language;
  }, [language]);

  React.useEffect(() => {
    const refreshLanguage = () => {
      if (ref.current.language !== Locale.current()) {
        console.log(`i18n language changed detect, previous=${ref.current.language}, current=${Locale.current()}`);
        setLanguage(Locale.current());
      }
    };

    refreshLanguage();
    const timer = setInterval(() => refreshLanguage(), 300);
    return () => clearInterval(timer);
  }, []);

  return language;
}
export {useSrsLanguage};

