() => {
 window.runLogoContrast = async theme => {
  if(!['broray','day','night'].includes(theme))throw Error('THEME');
  document.getElementById('logo-contrast-fixture')?.remove();
  document.documentElement.setAttribute('data-theme',theme);
  const area=document.createElement('section');area.id='logo-contrast-fixture';area.style.cssText='position:fixed;top:120px;left:290px;z-index:120;display:flex;gap:24px;padding:24px;background:var(--ui-surface);border:1px solid var(--ui-border);border-radius:12px';document.body.append(area);
  const rows=[];
  for(const embeddingScheme of ['light','dark']){
   const card=document.createElement('div');card.style.cssText='display:grid;gap:10px;min-width:140px';card.style.colorScheme=embeddingScheme;
   const label=document.createElement('span');label.textContent=theme+' / '+embeddingScheme;
   const box=document.createElement('span');box.className='route-card-logo route-card-logo-light';box.setAttribute('aria-hidden','true');
   const img=document.createElement('img');img.className='route-card-logo-image';img.alt='';img.src='/assets/images/routes/chatgpt.svg?v=contrast-check';box.append(img);card.append(label,box);area.append(card);
   await img.decode();await new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)));
   rows.push({embeddingScheme,imageScheme:getComputedStyle(img).colorScheme,background:getComputedStyle(box).backgroundColor,loaded:img.complete&&img.naturalWidth===180,imageRect:img.getBoundingClientRect().toJSON()});
  }
  return {theme,actualTheme:document.documentElement.getAttribute('data-theme'),rootScheme:getComputedStyle(document.documentElement).colorScheme,viewport:{width:innerWidth,height:innerHeight,dpr:devicePixelRatio},rows,assertion:'Count real screenshot pixels in returned image rectangles; do not rasterize img again in canvas.'};
 };
 return {ready:true};
}