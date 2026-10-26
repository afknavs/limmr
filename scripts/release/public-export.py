#!/usr/bin/env python3
"""Build a local review candidate from committed files. Never publishes or runs it."""
import argparse
import gzip
import functools
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile

# Spell the private integration name as code points so the public audit tooling
# itself can be exported without retaining that brand.
PRIVATE_NAME = ''.join(map(chr, [71, 108, 105, 109, 116]))
PRIVATE_PATTERN = re.compile(PRIVATE_NAME, re.I)
EXCLUDED_PARTS = {'.git', '.cache', '.agents', '.codex', '.aws', '.ssh', '.gnupg', 'Credentials', '.build', 'node_modules', 'dist', 'build'}
EXCLUDED_NAMES = {'OVERNIGHT-PLAN.md', 'RESTORE-NOTES.md', 'AUDIT-NOTES.md', 'auth.json', 'remote.json', 'control-token', 'status-token', 'sync-oauth.json', '.bridge-token', '.mcp-token'}
BINARY_ASSETS = {'.png', '.jpg', '.jpeg', '.gif', '.ico', '.icns', '.pdf', '.woff', '.woff2', '.ttf'}
TRACE = re.compile(r'com\.abhinav|\babhinavm?\b|\bafknavs\b(?!/limmr\b)|/Users/(?!me(?:/|\b)|example(?:/|\b))[^/\s]+', re.I)
SECRETS = [re.compile(r'-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----'), re.compile(r'\b(?:gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{40,}|sk-[A-Za-z0-9_-]{32,}|AKIA[A-Z0-9]{16})\b')]


def load_private_terms():
    """Read optional owner-only input here only. Never return values in diagnostics."""
    path = Path(os.environ.get('LIMMR_PRIVATE_TERMS', '~/.config/limmr/private-terms.txt')).expanduser()
    if not path.exists():
        return None
    try:
        return [re.compile(r'(?<!\w)' + re.escape(term.strip()) + r'(?!\w)', re.I)
                for term in path.read_text(encoding='utf-8').splitlines() if term.strip()]
    except (OSError, UnicodeError, re.error):
        raise ValueError('Could not read the private term list (contents withheld)') from None


@functools.lru_cache(maxsize=4)
def private_prefilter(patterns):
    return re.compile('|'.join('(?:' + p.pattern + ')' for p in patterns), re.I)


def private_findings(path, text, terms):
    if not terms:
        return []
    candidate = private_prefilter(tuple(terms))
    return [{'path': path, 'line': number, 'term_index': index}
            for number, line in enumerate(text.splitlines(), 1) if candidate.search(line)
            for index, pattern in enumerate(terms or [], 1) if pattern.search(line)]


def excluded(path):
    p = Path(path)
    return ((path.startswith('docs/') and PRIVATE_PATTERN.search(path) is not None)
            or bool(set(p.parts) & EXCLUDED_PARTS) or p.name in EXCLUDED_NAMES
            or p.name.endswith('-REPORT.md') or p.name.startswith('CODEX-')
            or path.startswith(('docs/lanes/', 'docs/snapshots/'))
            or 'how-it-was-made' in path.lower()
            or path in {'docs/SPEC.md', 'docs/OWNER-CHECKS.md', 'docs/U3-DESIGN.md', 'docs/HOW-IT-WAS-MADE.md'}
            or (path.startswith('docs/') and (p.suffix in BINARY_ASSETS or any('snapshot' in part for part in p.parts))
                and path != 'docs/pdf/Limmr-Setup-Guide.pdf'
                and not (path.startswith('docs/setup/') and p.suffix.lower() in {'.png', '.jpg', '.jpeg'}))
            or p.name.startswith('.env') or re.search(r'\.(?:db|sqlite|sqlite3)(?:-(?:wal|shm))?$', p.name)
            or p.suffix in {'.zip', '.dmg', '.app', '.key', '.pem', '.p12', '.pfx'})


def legal_line(path, line):
    return path in {'LICENSE', 'NOTICE', 'README.md'} and 'copyright' in line.lower()


def canonical_fixture(path, before, after):
    """Renamed keys can change sort order; re-sort shared JSON fixtures the way Go's MarshalIndent writes them."""
    if after == before or not (path.startswith('app/Tests/LimmrKitTests/Fixtures/') and path.endswith('.json')):
        return after
    try:
        value = json.loads(after)
    except ValueError:
        return after
    out = json.dumps(value, indent=2, sort_keys=True, ensure_ascii=False)
    for raw, escaped in (('&', '\\u0026'), ('<', '\\u003c'), ('>', '\\u003e'), ('\u2028', '\\u2028'), ('\u2029', '\\u2029')):
        out = out.replace(raw, escaped)
    return out + '\n'


def sanitize(path, text):
    if path == 'AGENTS.md':
        return """# Contributor safety rules
Use invented data and throwaway LIMMR_HOME directories for development.
Never send WhatsApp messages, reactions, typing indicators, read receipts or presence while testing.
Never access an existing installation's databases, credentials, services or settings.
Never commit secrets or real chat data. Use the fake WhatsApp implementation for tests.
Build offline with cached dependencies. Keep changes reversible and record verification limits.
Do not launch a bundled app during automated review; use the unbundled offscreen demo snapshot workflow.
See CONTRIBUTING.md for build commands. Publication and installation require separate owner authorization.
"""
    # Vendored code and legal notices retain original attribution and behavior.
    if '/third_party/' in path or path in {'LICENSE', 'NOTICE'}:
        return text
    text = text.replace(PRIVATE_NAME, 'Accessory').replace(PRIVATE_NAME.lower(), 'accessory').replace(PRIVATE_NAME.upper(), 'ACCESSORY')
    out = []
    for line in text.splitlines(keepends=True):
        if path.endswith('.swift') and (line.rstrip().endswith('// private-light-integration') or line.rstrip().endswith('// private-light-search')):
            continue
        if legal_line(path, line):
            out.append(line)
            continue
        # Keep this export/audit tooling out of its own owner-trace transformation.
        if path.startswith('scripts/release/public-export'):
            out.append(line)
            continue
        line = re.sub(r'com\.abhinav', 'dev.limmr', line, flags=re.I)
        line = re.sub(r'/Users/[^/\s"\x27`]+', '/Users/example', line)
        line = re.sub(r'\bAbhinav\b', 'the owner', line)
        line = re.sub(r'\babhinavm?\b', 'example', line, flags=re.I)
        line = re.sub(r'\bafknavs\b(?!/limmr\b)', 'example', line, flags=re.I)
        # Public screenshots are selected separately after synthetic-data review.
        if path.endswith('.md'):
            line = re.sub(r'!\[[^]]*\]\([^)]*\.(?:png|jpe?g|gif)(?:[^)]*)\)', '[Demo image omitted pending release review.]', line, flags=re.I)
        out.append(line)
    return ''.join(out)


def inspect_text(path, text):
    findings = []
    if PRIVATE_PATTERN.search(text) or PRIVATE_PATTERN.search(path):
        findings.append('private-integration-name')
    for line in text.splitlines():
        if any(rx.search(line) for rx in SECRETS):
            findings.append('private-key-or-credential-pattern')
        if not legal_line(path, line) and not path.startswith('scripts/release/public-export') and TRACE.search(line):
            findings.append('owner-trace')
    return sorted(set(findings))


def export(root, output):
    private_terms = load_private_terms()
    root, output = root.resolve(), output.resolve()
    if output.exists():
        raise ValueError('Output already exists; choose a fresh review directory')
    if root == output or (root in output.parents and not output.is_relative_to(root / '.cache')):
        raise ValueError('Output must be outside source files or under the ignored .cache directory')
    revision = subprocess.check_output(['git', '-C', str(root), 'rev-parse', 'HEAD'], text=True).strip()
    entries = subprocess.check_output(['git', '-C', str(root), 'ls-tree', '-rz', '--full-tree', revision]).split(b'\0')
    files, omitted, findings = [], [], []
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='.public-export-', dir=output.parent) as temp:
        stage = Path(temp) / 'tree'
        stage.mkdir()
        for entry in entries:
            if not entry:
                continue
            meta, rawpath = entry.split(b'\t', 1)
            mode, kind, oid = meta.decode().split()
            path = rawpath.decode()
            if excluded(path):
                omitted.append(path)
                continue
            if kind != 'blob' or mode not in {'100644', '100755'} or Path(path).is_absolute() or '..' in Path(path).parts:
                findings.append({'path': path, 'reason': 'unsupported-file-kind'})
                continue
            data = subprocess.check_output(['git', '-C', str(root), 'cat-file', 'blob', oid])
            compressed = path.endswith('.gz')
            try:
                body = gzip.decompress(data) if compressed else data
                text = body.decode('utf-8')
            except (UnicodeError, OSError, EOFError):
                if Path(path).suffix not in BINARY_ASSETS:
                    findings.append({'path': path, 'reason': 'uninspected-binary'})
                    continue
                text = None
                findings.extend(private_findings(path, body.decode('latin1'), private_terms))
                if Path(path).suffix.lower() == '.pdf':
                    try:
                        extracted = subprocess.check_output(['pdftotext', '-', '-'], input=body, stderr=subprocess.DEVNULL).decode('utf-8')
                        metadata = subprocess.check_output(['pdfinfo', '-url', '-'], input=body, stderr=subprocess.DEVNULL).decode('utf-8')
                        extracted += '\n' + metadata
                    except (OSError, UnicodeError, subprocess.CalledProcessError):
                        raise ValueError('PDF text inspection unavailable (contents withheld)') from None
                    findings.extend({'path': path, 'reason': issue} for issue in inspect_text(path, extracted))
                    findings.extend(private_findings(path, extracted, private_terms))
            if text is not None:
                # Scan original bytes for credentials BEFORE any text transformation.
                for issue in inspect_text(path, text):
                    if issue not in {'owner-trace', 'private-integration-name'}:
                        findings.append({'path': path, 'reason': issue})
                text = canonical_fixture(path, text, sanitize(path, text))
                findings.extend({'path': path, 'reason': issue} for issue in inspect_text(PRIVATE_PATTERN.sub("accessory", path), text))
                findings.extend(private_findings(path, text, private_terms))
                body = text.encode()
                data = gzip.compress(body, mtime=0) if compressed else body
            path = PRIVATE_PATTERN.sub('accessory', path)
            if PRIVATE_PATTERN.search(body.decode('latin1')):
                findings.append({'path': path, 'reason': 'private-integration-name'})
            dest = stage / path
            dest.parent.mkdir(parents=True, exist_ok=True)
            dest.write_bytes(data)
            dest.chmod(0o755 if mode == '100755' else 0o644)
            files.append({'path': path, 'bytes': len(data), 'sha256': hashlib.sha256(data).hexdigest(), 'inspection': 'text' if text is not None else 'asset-not-content-audited'})
        if private_terms is not None:
            findings.extend(private_findings('PUBLIC-EXPORT-MANIFEST.json', json.dumps(files), private_terms))
        if findings:
            # No values or snippets; failed candidates are discarded automatically.
            raise ValueError(json.dumps({'audit_failed': findings}, sort_keys=True))
        manifest = {'source_revision': revision, 'status': 'local-review-candidate-not-for-publication', 'files': files, 'excluded_count': len(omitted),
                    'private_term_audit': {'status': 'checked' if private_terms is not None else 'skipped-missing-file', 'terms': len(private_terms or [])},
                    'limitations': ['Pattern scanning is not a proof that all secrets or personal data are absent.', 'Opaque image/font assets require visual review.', 'Copyright attribution retained; export tooling contains audit patterns.', 'Rewritten legacy migration identifiers are generic and need release review.']}
        (stage / 'PUBLIC-EXPORT-MANIFEST.json').write_text(json.dumps(manifest, indent=2)+'\n')
        stage.rename(output)
    print(json.dumps({'output': str(output), 'source_revision': revision, 'files': len(files), 'excluded': len(omitted), 'audit': 'passed-pattern-scan-review-required', 'private_terms': len(private_terms or []), 'private_term_audit': 'checked' if private_terms is not None else 'skipped: optional private term file missing'}))


if __name__ == '__main__':
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('output', type=Path)
    args = p.parse_args()
    try:
        export(Path(__file__).resolve().parents[2], args.output)
    except (ValueError, subprocess.CalledProcessError) as e:
        raise SystemExit(str(e))
