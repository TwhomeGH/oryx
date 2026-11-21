// Locale-aware back link, matching the other player pages: the console expects /mgmt/<lang>/...
(function () {
  try {
    const saved = JSON.parse(localStorage.getItem('ORYX_LOCALE') || 'null');
    const lang = (saved && saved.lang) || 'zh';
    const el = document.getElementById('navBack');
    if (el) el.href = '/mgmt/' + encodeURIComponent(lang) + '/routers-console';
  } catch (e) { /* keep the default */ }
})();
