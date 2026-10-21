// Alive: eases a light direction toward the pointer (or, with no pointer,
// toward where the sun is at this hour), and gives buttons and cards a
// sheen where the pointer is. Off under prefers-reduced-motion.
(function(){
  const root=document.documentElement, reduce=matchMedia('(prefers-reduced-motion: reduce)');
  const st={pointer:true,sheen:true,clock:true,k:1,hour:new Date().getHours()+new Date().getMinutes()/60};
  let tx=0,ty=-1,lx=0,ly=-1,raf=0,hasPointer=false;
  // cards ease slowly: position follows at a walk, brightness fades in and out
  const cards=new Map(); let craf=0;
  function breathe(){ if(!craf) craf=requestAnimationFrame(cstep); }
  function cstep(){ craf=0; let moving=false;
    for(const [el,o] of cards){ o.x+=(o.tx-o.x)*0.05; o.y+=(o.ty-o.y)*0.05; o.l+=(o.tl-o.l)*0.04;
      el.style.setProperty('--cx',o.x.toFixed(1)+'px'); el.style.setProperty('--cy',o.y.toFixed(1)+'px'); el.style.setProperty('--cl',o.l.toFixed(3));
      if(Math.abs(o.tx-o.x)>0.5||Math.abs(o.ty-o.y)>0.5||Math.abs(o.tl-o.l)>0.004) moving=true; else if(o.tl===0){ o.l=0; el.style.setProperty('--cl','0'); } }
    if(moving) breathe(); }
  function sun(){ // 6am light from the left, noon from above, 6pm from the right
    const t=Math.max(-1,Math.min(1,(st.hour-12)/6)); return [t*0.8,-1+Math.abs(t)*0.35]; }
  function target(){ if(st.pointer&&hasPointer) return; if(st.clock){[tx,ty]=sun()} else {tx=0;ty=-1} }
  function tick(){ lx+=(tx-lx)*0.12; ly+=(ty-ly)*0.12;
    root.style.setProperty('--lx',lx.toFixed(3)); root.style.setProperty('--ly',ly.toFixed(3));
    if(Math.abs(tx-lx)>0.002||Math.abs(ty-ly)>0.002) raf=requestAnimationFrame(tick); else raf=0; }
  function go(){ if(!raf) raf=requestAnimationFrame(tick); }
  addEventListener('pointermove',e=>{ if(reduce.matches) return;
    if(e.pointerType==='mouse'&&st.pointer){ hasPointer=true;
      // the light sits at the pointer, a little above the page
      tx=Math.max(-1,Math.min(1,(e.clientX/innerWidth-0.5)*2)); ty=Math.max(-1,Math.min(1,(e.clientY/innerHeight-0.5)*2))*0.6-0.4; go(); }
    if(!st.sheen) return;
    // the button and the card it sits in each take the pointer in their own coordinates
    const b=e.target.closest('[rst-btn]');
    if(b){ const r=b.getBoundingClientRect(); b.style.setProperty('--mx',(e.clientX-r.left)+'px'); b.style.setProperty('--my',(e.clientY-r.top)+'px'); }
    const c=e.target.closest('[rst-box],[rst-card],[rst-stats]');
    for (const k of cards.keys()) if(k!==c) cards.get(k).tl=0;
    if(c){ const r=c.getBoundingClientRect(); let o=cards.get(c);
      if(!o){ o={x:e.clientX-r.left,y:e.clientY-r.top,l:0}; cards.set(c,o); }
      o.tx=e.clientX-r.left; o.ty=e.clientY-r.top; o.tl=1; }
    breathe();
  },{passive:true});
  addEventListener('pointerdown',e=>{ if(reduce.matches||e.pointerType==='mouse') return;
    const b=e.target.closest('[rst-btn]');
    if(b){ const r=b.getBoundingClientRect(); b.style.setProperty('--mx',(e.clientX-r.left)+'px'); b.style.setProperty('--my',(e.clientY-r.top)+'px'); b.style.setProperty('--on','1');
      const off=()=>{b.style.removeProperty('--on'); removeEventListener('pointerup',off); removeEventListener('pointercancel',off);}; addEventListener('pointerup',off); addEventListener('pointercancel',off); }
    const c=e.target.closest('[rst-box],[rst-card],[rst-stats]');
    if(c){ const r=c.getBoundingClientRect(); let o=cards.get(c); if(!o){o={x:e.clientX-r.left,y:e.clientY-r.top,l:0};cards.set(c,o);}
      o.tx=e.clientX-r.left; o.ty=e.clientY-r.top; o.tl=1; breathe();
      setTimeout(()=>{o.tl=0;breathe();},1400); }
  },{passive:true});
  document.addEventListener('pointerleave',()=>{hasPointer=false;for(const o of cards.values())o.tl=0;breathe();target();go();});
  // mockup controls
  const p=document.createElement('details'); p.className='alive-panel'; if(innerWidth>700) p.open=true; p.innerHTML=
   '<summary><b>Mockup controls</b></summary>'+
   '<label><input type=checkbox data-k=pointer checked> Light follows the pointer</label>'+
   '<label><input type=checkbox data-k=sheen checked> Sheen under the pointer</label>'+
   '<label><input type=checkbox data-k=clock checked> Light from the hour, without a pointer</label>'+
   '<label>Hour <input type=range min=6 max=18 step=0.25 data-k=hour></label>'+
   '<label>Strength <input type=range min=0 max=4 step=0.5 value=1 data-k=k> <span data-v></span></label>';
  document.addEventListener('DOMContentLoaded',()=>{ document.body.append(p);
    p.querySelector('[data-k=hour]').value=Math.min(18,Math.max(6,st.hour));
    const v=p.querySelector('[data-v]'); v.textContent='×1, the real setting';
    p.addEventListener('input',e=>{const k=e.target.dataset.k; if(!k) return;
      if(e.target.type==='checkbox') st[k]=e.target.checked; else st[k]=+e.target.value;
      if(k==='k'){ root.style.setProperty('--k',st.k); v.textContent='×'+st.k+(st.k===1?', the real setting':st.k>1?', exaggerated to see it':''); }
      if(k==='sheen') root.style.setProperty('--ss', st.sheen?'calc(var(--k) / 4)':'0');
      if(k==='hour'||k==='clock'){hasPointer=false;} target(); go(); });
    target(); go(); });
})();
