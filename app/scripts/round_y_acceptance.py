#!/usr/bin/env python3
"""Sequential offscreen Round Y acceptance captures using synthetic data only."""
import hashlib,json,os,struct,subprocess,tempfile
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
OUT=ROOT.parent/'deliverables/round-y-acceptance'
PAGES=['y1-navigation-stress','y2-home-circles','y2-home-open','y2-home-suggestions','y2-home-kept','y2-home-dismissed','y2-home-undone','y2-activity-suggested','q4-task-study','y45-groups','y45-group','y45-custom','y45-initials','y45-contacts','y7-dot-never','y7-dot-healthy','y7-dot-error','y7-dot-review']
OUT.mkdir(parents=True,exist_ok=True)
results=json.loads((OUT/'manifest.json').read_text()) if (OUT/'manifest.json').exists() else []
completed={(x['page'],x['theme']) for x in results}
with tempfile.TemporaryDirectory(prefix='limmr-round-y-acceptance-') as home:
 for page in PAGES:
  for theme in (['light'] if page=='y1-navigation-stress' else ['light','dark','contrast']):
   if (page,theme) in completed: continue
   path=OUT/f'{page}-{theme}.png'
   args=[str(ROOT/'app/.build/debug/Limmr'),'--snapshot',str(path),'--page',page,'--titled-window','--size','1280x820']
   if theme=='dark':args+=['--dark']
   if theme=='contrast':args+=['--increase-contrast']
   try:
    run=subprocess.run(args,env=dict(os.environ,LIMMR_HOME=home),stdin=subprocess.DEVNULL,capture_output=True,timeout=180 if page=="y1-navigation-stress" else 60)
   except subprocess.TimeoutExpired as e:
    (OUT/f'{page}-{theme}.log').write_bytes((e.stdout or b'')+(e.stderr or b''))
    results.append({'page':page,'theme':theme,'exit_code':None,'status':'timeout-180s' if page=='y1-navigation-stress' else 'timeout-60s','file':path.name})
    (OUT/'manifest.json').write_text(json.dumps(results,indent=2)+'\n')
    print(page,theme,'timeout',flush=True)
    continue
   (OUT/f'{page}-{theme}.log').write_bytes(run.stdout+run.stderr)
   item={'page':page,'theme':theme,'exit_code':run.returncode,'file':path.name}
   if run.returncode==0:
    data=path.read_bytes();assert data[:8]==b'\x89PNG\r\n\x1a\n' and struct.unpack('>II',data[16:24])==(1280,820)
    item['sha256']=hashlib.sha256(data).hexdigest()
   results.append(item);(OUT/'manifest.json').write_text(json.dumps(results,indent=2)+'\n')
   print(page,theme,run.returncode,flush=True)

if any(x.get("exit_code")!=0 for x in results):raise SystemExit(1)
