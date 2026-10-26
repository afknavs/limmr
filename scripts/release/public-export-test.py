#!/usr/bin/env python3
"""Offline fixture regressions for the export boundary; never opens real data."""
import importlib.util
import gzip
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

spec=importlib.util.spec_from_file_location('exporter',Path(__file__).with_name('public-export.py'))
module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)

class ExportTests(unittest.TestCase):
    def setUp(self):
        self.home = tempfile.TemporaryDirectory()
        self.addCleanup(self.home.cleanup)
        self.environment = patch.dict('os.environ', {'LIMMR_PRIVATE_TERMS': str(Path(self.home.name)/'missing-terms')})
        self.environment.start()
        self.addCleanup(self.environment.stop)

    def test_private_terms_use_literal_whole_words_and_withhold_values(self):
        with tempfile.TemporaryDirectory() as d:
            terms=Path(d)/'private.txt';terms.write_text('Maya Rao\nC++ club\n')
            with patch.dict('os.environ', {'LIMMR_PRIVATE_TERMS': str(terms)}):
                root=self.repo(d,{'source.txt':'Not Maya Raoul.\nMeet MAYA RAO.\nC++ club meets.\n'})
                with self.assertRaises(ValueError) as caught: module.export(root,Path(d)/'candidate')
                error=str(caught.exception)
                self.assertNotIn('Maya',error);self.assertNotIn('club',error)
                import json
                self.assertEqual(json.loads(error)['audit_failed'],[
                    {'path':'source.txt','line':2,'term_index':1},
                    {'path':'source.txt','line':3,'term_index':2}])
                self.assertFalse((Path(d)/'candidate').exists())

    def test_private_terms_missing_file_is_explicitly_skipped(self):
        with tempfile.TemporaryDirectory() as d:
            root=self.repo(d,{'README.md':'Invented sample'})
            out=Path(d)/'candidate';module.export(root,out)
            import json
            self.assertEqual(json.loads((out/'PUBLIC-EXPORT-MANIFEST.json').read_text())['private_term_audit']['status'],'skipped-missing-file')

    def test_setup_assets_have_an_explicit_allowlist(self):
        for path in ['docs/pdf/Limmr-Setup-Guide.pdf','docs/setup/qr.png','docs/setup/client.jpg']:
            self.assertFalse(module.excluded(path),path)
        for path in ['docs/pdf/other.pdf','docs/images/other.png','docs/setup/file.pdf','docs/setup/session.db','docs/HOW-IT-WAS-MADE.md','docs/pdf/src/how-it-was-made.html']:
            self.assertTrue(module.excluded(path),path)

    def repo(self, home, files):
        root=Path(home)/'source';root.mkdir()
        subprocess.run(['git','init','-q',str(root)],check=True)
        for name,data in files.items():
            p=root/name;p.parent.mkdir(parents=True,exist_ok=True);p.write_bytes(data) if isinstance(data,bytes) else p.write_text(data)
        subprocess.run(['git','-C',str(root),'add','.'],check=True)
        subprocess.run(['git','-C',str(root),'-c','user.name=Fixture','-c','user.email=fixture@example.invalid','commit','-qm','fixture'],check=True)
        return root
    def test_excludes_runtime_and_internal_files_and_uses_commit(self):
        with tempfile.TemporaryDirectory() as d:
            root=self.repo(d,{'README.md':'com.abhinav.test /Users/abhinavm/project\n','docs/lanes/plan.md':'private plan','docs/SPEC.md':'Owner biography that must not export','runtime/hub.db':'fake database','.bridge-token':'fake denied file','docs/snapshots/a.png':'fake snapshot','docs/images/legacy/a.jpg':'fake legacy snapshot','integrations/chatgpt/instructions.md':'retained','app/Tests/LimmrKitTests/Fixtures/tasks/a.json':'{}'})
            (root/'README.md').write_text('UNCOMMITTED')
            out=Path(d)/'candidate';module.export(root,out)
            self.assertEqual((out/'README.md').read_text(),'dev.limmr.test /Users/example/project\n')
            for name in ['docs/lanes/plan.md','docs/SPEC.md','runtime/hub.db','.bridge-token','docs/snapshots/a.png','docs/images/legacy/a.jpg']:
                self.assertFalse((out/name).exists())
            self.assertTrue((out/'app/Tests/LimmrKitTests/Fixtures/tasks/a.json').is_file())
            with self.assertRaises(ValueError):module.export(root,out)
    def test_rejects_credentials_without_echoing_values(self):
        with tempfile.TemporaryDirectory() as d:
            fake='gh'+'p_'+'X'*36
            root=self.repo(d,{'source.txt':fake});out=Path(d)/'candidate'
            with self.assertRaises(ValueError) as caught:module.export(root,out)
            self.assertNotIn(fake,str(caught.exception));self.assertFalse(out.exists())
    def test_compressed_text_is_inspected_not_treated_as_opaque(self):
        with tempfile.TemporaryDirectory() as d:
            fake='gh'+'p_'+'Y'*36
            root=self.repo(d,{'fixture.json.gz':gzip.compress(fake.encode())})
            with self.assertRaises(ValueError) as caught:module.export(root,Path(d)/'candidate')
            self.assertNotIn(fake,str(caught.exception))
    def test_private_integration_has_no_export_exceptions(self):
        brand = module.PRIVATE_NAME
        self.assertIn('private-integration-name', module.inspect_text('LICENSE', brand))
        self.assertIn('private-integration-name', module.inspect_text('scripts/release/public-export.py', brand.lower()))
        with tempfile.TemporaryDirectory() as d:
            root=self.repo(d,{'docs/integrations/'+brand+'.md':'private guide',
                'engine/'+brand.lower()+'.go':'type '+brand+' struct {}',
                'app/Settings.swift':'Text("'+brand+' light")\nif enabled { privateView() } // private-light-integration\n'})
            out=Path(d)/'candidate';module.export(root,out)
            self.assertFalse((out/'docs/integrations'/ (brand+'.md')).exists())
            for path in out.rglob('*'):
                if path.is_file():
                    self.assertNotIn(brand.lower(), str(path.relative_to(out)).lower())
                    self.assertNotIn(brand.lower().encode(), path.read_bytes().lower())
            self.assertNotIn('privateView', (out/'app/Settings.swift').read_text())
    def test_preserves_copyright_and_rejects_symlinks(self):
        with tempfile.TemporaryDirectory() as d:
            root=self.repo(d,{'LICENSE':'Copyright 2026 Abhinav\n'})
            link=root/'link';link.symlink_to('/outside-fixture')
            subprocess.run(['git','-C',str(root),'add','link'],check=True)
            subprocess.run(['git','-C',str(root),'-c','user.name=Fixture','-c','user.email=fixture@example.invalid','commit','-qm','link'],check=True)
            with self.assertRaises(ValueError):module.export(root,Path(d)/'candidate')
            self.assertEqual(module.sanitize('LICENSE','Copyright 2026 Abhinav\n'),'Copyright 2026 Abhinav\n')

if __name__=='__main__':unittest.main()
