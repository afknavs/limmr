const Q=new URLSearchParams(location.search),DARK=Q.get('m')=='dark';document.documentElement.dataset.m=DARK?'dark':'light';document.documentElement.dataset.t=Q.get('t')||'paper';
const P={home:'M3 11l9-7 9 7v9h-6v-6H9v6H3z',grid:'M4 4h7v7H4zM13 4h7v7h-7zM4 13h7v7H4zM13 13h7v7h-7z',
people:'M9 11a3.5 3.5 0 100-7 3.5 3.5 0 000 7zM2.5 20c.5-3.5 3-5.5 6.5-5.5s6 2 6.5 5.5M16 4.5a3.5 3.5 0 010 6.5M18 14.8c2 .7 3.2 2.5 3.5 5.2',
pulse:'M3 12h4l2.5-7 4 14 2.5-7h5',cap:'M2 9l10-5 10 5-10 5zM6 11.5V16c1.5 1.5 3.5 2.3 6 2.3s4.5-.8 6-2.3v-4.5',
house:'M4 11l8-6.5 8 6.5M6 9.5V20h12V9.5',heart:'M12 20s-7.5-4.6-7.5-10A4.3 4.3 0 0112 7.3 4.3 4.3 0 0119.5 10c0 5.4-7.5 10-7.5 10z',
bag:'M4 8h16v11H4zM9 8V5.5h6V8',user:'M12 12a4 4 0 100-8 4 4 0 000 8zM4.5 20.5c.8-3.6 3.6-5.5 7.5-5.5s6.700 1.900 7.5 5.5',
gear:'M4 7h10M18 7h2M4 17h2M10 17h10M16 7a2 2 0 11-4 0 2 2 0 014 0zM10 17a2 2 0 11-4 0 2 2 0 014 0z',search:'M11 18a7 7 0 100-14 7 7 0 000 14zM20 20l-4-4',
chev:'M9 6l6 6-6 6',clock:'M12 21a9 9 0 100-18 9 9 0 000 18zM12 7v5l3 2',check:'M5 12.5l4.500 4.500L19 7.500',term:'M4 5h16v14H4zM7.500 10l2.500 2-2.500 2M12.500 14h4',
code:'M8 7l-5 5 5 5M16 7l5 5-5 5M13.500 5l-3 14',chat:'M4 5h16v11H10l-4 3.500V16H4z',doc:'M7 3.500h7l4 4V20.500H7zM14 3.500v4h4M10 12.500h5M10 16h5',
sort:'M7 5v14M7 19l-3-3M7 19l3-3M17 19V5M17 5l-3 3M17 5l3 3',filter:'M4 6h16M7 12h10M10 18h4',plus:'M12 5v14M5 12h14',
list:'M8 6.500h12M8 12h12M8 17.500h12M4 6.500h.01M4 12h.01M4 17.500h.01',pin:'M12 17v4M8 4h8l-1 6 3 4H6l3-4z',spark:'M12 3.500l2 6 6 2.500-6 2.500-2 6-2-6-6-2.500 6-2.500z',
map:'M12 12m-2.500 0a2.500 2.500 0 105 0 2.500 2.500 0 10-5 0M5 5.500h.01M19 6.500h.01M6 18.500h.01M18.500 17.500h.01M12 9.500L5 5.500M14 13.500l4.500 4',
cal:'M5 6h14v14H5zM5 10h14M9 3.500V7M15 3.500V7',play:'M8 5.500v13l10-6.500z',more:'M6 12h.01M12 12h.01M18 12h.01',mark:'M12 12m-8 0a8 8 0 1016 0 8 8 0 10-16 0M12 12m-3 0a3 3 0 106 0 3 3 0 10-6 0'};
const ic=(n,s)=>`<svg class="ic" viewBox="0 0 24 24"${s?` style="width:${s}px;height:${s}px"`:''}><path d="${P[n]}"/></svg>`;
const POS=['18% 30%','70% 40%','40% 70%','85% 80%','30% 20%','60% 60%','10% 85%','90% 20%','50% 35%'];
const av=(i,s=22)=>`<i class="av" style="width:${s}px;height:${s}px;background-image:url(assets/p${i%16+1}.jpg);background-position:${POS[(i*7)%9]}"></i>`;
const stack=(a,s=20)=>`<span class="stack">${a.map(i=>av(i,s)).join('')}</span>`;
const steps=(d,t,live)=>`<span class="steps">${Array.from({length:t},(_,k)=>`<i class="${k<d?'d':(k==d&&live?'n':'')}"></i>`).join('')}</span>`;
const GLY=c=>`<g fill="${c}"><rect x="22.900" y="12.200" width="18.200" height="62.800" rx="3.600"/><rect x="17.800" y="70" width="64.400" height="17.800" rx="3.600"/><rect x="63.300" y="46.700" width="18.900" height="41.100" rx="3.600"/><path d="M41.100 70h3a3 3 0 0 1-3-3zM63.300 70h-3a3 3 0 0 0 3-3zM22.900 70h-3a3 3 0 0 0 3-3z"/></g><rect x="49.600" y="17.800" width="23.700" height="23.300" rx="2" fill="#F4461F"/>`;
const mark=(s)=>`<svg width="${s}" height="${s}" viewBox="12 8 76 84">${GLY('var(--ink)')}</svg>`;
const appicon=(s,d)=>`<svg width="${s}" height="${s}" viewBox="0 0 100 100"><defs><linearGradient id="t${d?1:0}" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="${d?'#2B2927':'#FCFAF6'}"/><stop offset="1" stop-color="${d?'#191817':'#F3EEE5'}"/></linearGradient></defs><rect width="100" height="100" rx="23" fill="url(#t${d?1:0})"/><rect x=".500" y=".500" width="99" height="99" rx="22.500" fill="none" stroke="${d?'rgba(255,255,255,.09)':'rgba(0,0,0,.06)'}"/>${GLY(d?'#F4EFE7':'#141413')}</svg>`;
function shell(active){const nav=(k,i,t,e,c)=>`<a class="nav${k==active?' on':''}">${ic(i)}${t}${e?`<em class="${c||''}">${e}</em>`:''}</a>`;
const c=(k,i,t)=>`<a class="nav sub${k==active?' on':''}">${ic(i)}${t}</a>`;
document.body.insertAdjacentHTML('afterbegin',`<aside><div class="lights"><i></i><i></i><i></i></div>
<div class="logo">${mark(24)}Limmr</div>
${nav('home','home','Home')}${nav('tasks','grid','Tasks','2','sq')}${nav('people','people','People')}${nav('activity','pulse','Activity')}
<h6>Circles</h6>${c('uni','cap','University')}${c('fam','house','Family')}${c('close','heart','Close people')}${c('work','bag','Design Collective')}${c('you','user','You')}
<h6>Your AIs</h6><div class="ai">${ic('term')}Codex<em>working <i class="dot live"></i></em></div><div class="ai">${ic('code')}Claude Code<em>1 h</em></div><div class="ai">${ic('chat')}Dot<em>working <i class="dot live"></i></em></div>
<div class="foot">${c('set','gear','Settings')}<div class="conn"><i class="dot ok"></i>WhatsApp connected</div></div></aside>`)}
