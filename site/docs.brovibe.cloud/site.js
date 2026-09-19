const DOCS_SEARCH_INDEX = [{"url": "/", "title": "Главная документации", "summary": "Выбор между BROray и BROray-Light", "keywords": "выбор продукт документация"}, {"url": "/broray/", "title": "BROray", "summary": "Обзор полного клиента Xray для Keenetic", "keywords": "bro ray полный xray"}, {"url": "/broray/start/", "title": "Установка BROray", "summary": "Первая установка, Entware и терминал", "keywords": "установка entware ssh терминал"}, {"url": "/broray/webui/", "title": "WebUI BROray", "summary": "Главная, Серверы, Подписки, Маршруты, DNS, Keenetic, Xray", "keywords": "webui серверы подписки маршруты dns keenetic xray"}, {"url": "/broray/scenarios/", "title": "Сценарии BROray", "summary": "Подписка, сервер, резерв, маршруты, DNS-over-TLS, KeenDNS", "keywords": "подписка сервер автопереключение маршрут dot keendns"}, {"url": "/broray/maintenance/", "title": "Обслуживание BROray", "summary": "Обновление, переустановка, восстановление и удаление", "keywords": "обновление переустановка удалить восстановление"}, {"url": "/broray/troubleshooting/", "title": "Решение проблем BROray", "summary": "WebUI, вход, серверы, подписки, маршруты, DNS и обновление", "keywords": "не работает ошибка диагностика webui xray подписка"}, {"url": "/broray/releases/", "title": "История версий BROray", "summary": "Что изменилось в версиях BROray", "keywords": "релиз версия changelog 3.1.1"}, {"url": "/broray/reference/", "title": "Справочник BROray", "summary": "Требования, совместимость, безопасность и компоненты", "keywords": "требования совместимость безопасность sha"}, {"url": "/broray-light/", "title": "BROray-Light", "summary": "Обзор VLESS-only редакции", "keywords": "light vless"}, {"url": "/broray-light/start/", "title": "Установка BROray-Light", "summary": "Совместимость, Entware и чистая установка", "keywords": "light установка entware"}, {"url": "/broray-light/webui/", "title": "WebUI BROray-Light", "summary": "Главная, Серверы и Подписки", "keywords": "light webui главная серверы подписки"}, {"url": "/broray-light/scenarios/", "title": "Сценарии BROray-Light", "summary": "VLESS, подписка, сервер и резерв", "keywords": "light vless подписка сервер резерв"}, {"url": "/broray-light/maintenance/", "title": "Обслуживание BROray-Light", "summary": "Обновление Light и Xray, удаление", "keywords": "light обновление xray удалить"}, {"url": "/broray-light/troubleshooting/", "title": "Решение проблем BROray-Light", "summary": "WebUI, VLESS, подписки и обновление", "keywords": "light ошибка не работает диагностика"}, {"url": "/broray-light/releases/", "title": "История версий BROray-Light", "summary": "Релизы Light", "keywords": "light релиз версия changelog"}, {"url": "/broray-light/reference/", "title": "Справочник BROray-Light", "summary": "Совместимость и ограничения VLESS-only", "keywords": "light совместимость ограничения"}];

(function () {
  const normalize = (s) => (s || '').toLocaleLowerCase('ru-RU').replace(/ё/g,'е').trim();
  function ensureDialog() {
    let d=document.getElementById('docs-search-dialog');
    if(d) return d;
    d=document.createElement('dialog'); d.id='docs-search-dialog'; d.className='search-dialog';
    d.innerHTML='<form method="dialog" class="search-shell"><div class="search-head"><label for="docs-search-input">Поиск по документации</label><button class="search-close" value="cancel" aria-label="Закрыть">×</button></div><input id="docs-search-input" class="search-input" type="search" autocomplete="off" placeholder="Например: подписка не обновляется"><div id="docs-search-results" class="search-results" aria-live="polite"></div></form>';
    document.body.appendChild(d);
    const input=d.querySelector('#docs-search-input'), box=d.querySelector('#docs-search-results');
    const render=()=>{
      const q=normalize(input.value);
      const terms=q.split(/\s+/).filter(Boolean);
      let rows=DOCS_SEARCH_INDEX.map(x=>{
        const title=normalize(x.title), hay=normalize(x.title+' '+x.summary+' '+x.keywords);
        const score=terms.reduce((n,t)=>n+(title.includes(t)?3:hay.includes(t)?1:0),0);
        return {...x,score};
      });
      if(terms.length) rows=rows.filter(x=>x.score>0).sort((a,b)=>b.score-a.score || a.title.localeCompare(b.title,'ru'));
      box.innerHTML=rows.slice(0,10).map(x=>'<a class="search-result" href="'+x.url+'"><strong>'+x.title+'</strong><span>'+x.summary+'</span></a>').join('') || '<p class="search-empty">Ничего не найдено. Попробуйте одно ключевое слово или откройте раздел «Решение проблем».</p>';
    };
    input.addEventListener('input',render); d.addEventListener('close',()=>{input.value=''; render();}); render();
    return d;
  }
  document.querySelectorAll('[data-docs-search]').forEach(btn=>btn.addEventListener('click',()=>{const d=ensureDialog(); d.showModal(); setTimeout(()=>d.querySelector('#docs-search-input').focus(),0);}));
  document.addEventListener('keydown',e=>{if((e.ctrlKey||e.metaKey)&&e.key.toLowerCase()==='k'){e.preventDefault(); const d=ensureDialog(); if(!d.open)d.showModal(); d.querySelector('#docs-search-input').focus();}});
})();
