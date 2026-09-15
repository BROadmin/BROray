'use strict';
(() => {
  const $ = id => document.getElementById(id);
  const states = {
    checking: {type:'Проверка серверов',badge:'Выполняется',summary:'Проверяются доступность и качество соединения с серверами.',cancel:true},
    fetching: {type:'Обновление подписки',badge:'Выполняется',summary:'Загружается список серверов подписки.',cancel:true},
    idle: {type:'',badge:'Нет активных',summary:'Активных фоновых операций нет.'},
    ambiguous: {type:'Не удалось определить',badge:'Нужна проверка',summary:'BROray обнаружил незавершённую операцию, состояние которой не удалось подтвердить.',recover:true},
    protected: {type:'Обслуживание Xray',badge:'Завершение изменений',summary:'Применяются изменения. Дождитесь завершения этого этапа.'},
    unavailable: {type:'',badge:'Данные недоступны',summary:'Не удалось прочитать состояние операций. Повторите обновление.'}
  };
  let current='checking',paused=false,cancelRequested=false;
  const events=[{time:'12:34:02',message:'Начата автоматическая проверка серверов.'},{time:'12:34:01',message:'Завершено обновление подписки.'}];
  function event(message) {events.unshift({time:'12:35:26',message});events.splice(8);renderEvents();}
  function renderEvents() {
    $('event-list').replaceChildren(...events.map(e=>{
      const li=document.createElement('li'),time=document.createElement('time'),message=document.createElement('span');
      time.textContent=e.time;message.textContent=e.message;li.append(time,message);return li;
    }));
  }
  function render() {
    const s=states[current];
    $('ops-badge').textContent=cancelRequested?'Остановка запрошена':s.badge;
    $('ops-summary').textContent=s.summary;
    $('ops-type').textContent=s.type;
    $('ops-details').hidden=!s.type;
    $('cancel').hidden=!s.cancel;$('cancel').disabled=cancelRequested;
    $('recover').hidden=!s.recover;
    $('automation-badge').textContent=paused?'На паузе':'Включена';
    $('stop-all').disabled=paused;$('resume').hidden=!paused;
    renderEvents();
  }
  function cancel() {
    if (!states[current].cancel || cancelRequested) return;
    cancelRequested=true;
    $('operation-feedback').textContent='Запрос остановки отправлен. BROray ожидает безопасного завершения операции.';
    event('Запрошена остановка фоновой операции.');render();
  }
  function report() {
    return {schemaVersion:0,reportKind:'broray-preview',demonstration:true,complete:false,routerAccessed:false,
      unavailable:['Все сведения о реальном роутере'],scenario:current,automationPaused:paused,cancelRequested,events};
  }
  $('theme').addEventListener('change',e=>{document.documentElement.dataset.theme=e.target.value;});
  $('scenario').addEventListener('change',e=>{
    current=e.target.value;cancelRequested=false;$('operation-feedback').textContent='';render();
  });
  $('cancel').addEventListener('click',cancel);
  $('recover').addEventListener('click',()=>{
    $('operation-feedback').textContent='Состояние владельца остаётся неподтверждённым. Блокировка сохранена. Скачайте диагностический отчёт.';
    event('Восстановление отложено: состояние операции не подтверждено.');
  });
  $('stop-all').addEventListener('click',()=>{
    paused=true;cancel();
    $('automation-feedback').textContent=states[current].cancel?'Автоматика на паузе. Ожидается завершение текущей операции.':
      current==='protected'?'Автоматика на паузе. Защищённый этап продолжает выполняться.':
      current==='ambiguous'||current==='unavailable'?'Автоматика на паузе. Состояние текущих операций требует проверки.':'Автоматика на паузе.';
    event('Новые автоматические операции поставлены на паузу.');render();
  });
  $('resume').addEventListener('click',()=>{paused=false;$('automation-feedback').textContent='Автоматика возобновлена с прежними настройками.';event('Автоматика возобновлена.');render();});
  $('refresh').addEventListener('click',()=>{$('operation-feedback').textContent='Демонстрационный снимок обновлён.';render();});
  $('journal-refresh').addEventListener('click',()=>{$('report-feedback').textContent='Демонстрационный журнал обновлён.';renderEvents();});
  $('copy').addEventListener('click',async()=>{
    try {await navigator.clipboard.writeText(JSON.stringify(report(),null,2));$('report-feedback').textContent='Демонстрационная диагностика скопирована.';}
    catch {$('report-feedback').textContent='Копирование недоступно. Скачайте диагностический отчёт.';}
  });
  $('download').addEventListener('click',()=>{
    const url=URL.createObjectURL(new Blob([JSON.stringify(report(),null,2)],{type:'application/json'}));
    const a=document.createElement('a');a.href=url;a.download='BROray-3.1.1-preview-report.json';a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);
    $('report-feedback').textContent='Скачан пример отчёта с демонстрационными данными.';
  });
  render();
})();
