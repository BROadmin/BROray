#!/usr/bin/env python3
"""Offline contract tests for permanent BROray FAQ URLs and docs integrity."""
import hashlib
import json
import re
import unittest
from html.parser import HTMLParser
from pathlib import Path
from urllib.parse import unquote, urlsplit

ROOT = Path(__file__).resolve().parents[1]
SITE = ROOT / 'site' / 'docs.brovibe.cloud'
FAQ_ROOT = SITE / 'broray' / 'faq'
CORE_SLUGS = {
    '', 'login', 'home', 'servers', 'reserve', 'subscriptions', 'dns',
    'routes', 'routes-custom', 'routes-catalog', 'routes-domains',
    'routes-blocking', 'keenetic', 'xray', 'maintenance', 'troubleshooting',
}
# Previously public fragment IDs must remain valid through FAQ updates.
LEGACY_ANCHORS = {
    'broray/index.html': 'getting-started changes features installation subscription-options-357',
    'broray/start/index.html': 'update-351 update-359-heading remote-webui remote-webui-heading',
    'broray/beginner-installation/index.html': 'update-351 update-359-heading remote-webui remote-webui-heading',
    'broray/webui/index.html': 'login home servers subscriptions subscription-formats-355 import-options-357 import-353 dns warnings-and-filter-dns routes routes-custom routes-catalog routes-domains routes-blocks keenetic-xray xray maintenance-354 operations',
    'broray/scenarios/index.html': 'first-server subscription fallback routes dot keendns subscription-formats-355 subscription-options-357',
    'broray/maintenance/index.html': 'update modules xray-maintenance xray-core-remove backup recover cleanup uninstall',
    'broray/troubleshooting/index.html': 'webui login keeneticos-52-short-password auth-502-503 install space traffic udp quality subscription routes update legacy legacy32-uninstall keendns-only keendns-clean-install stale-cache first-empty support import-warnings-357 dns-filter-observation',
    'broray/releases/index.html': 'release-3-5-9 title-3-5-9 release-3-5-8 title-3-5-8 release-3-5-7 title-3-5-7 limitations-357 release-3-5-6 title-3-5-6 limitations-356 release-3-5-5 title-3-5-5 limitations-355 release-3-5-4 title-3-5-4 limitations-354 release-3-5-3 title-3-5-3 limitations-353 release-3-5-2 title-3-5-2 release-3-5-1 title-3-5-1 new release-3-5-0 changes features install-3-5-0 requirements verification notes history-summary v3-2-0-r12 history',
    'broray/reference/index.html': 'requirements keeneticos-components go architectures xray protocols components paths signature policies limitations-353',
}
VERSION_RE = re.compile(r'(?<![\d.])[vV]?\d{1,2}\.\d{1,2}\.\d{1,3}(?![\d.])')


class Document(HTMLParser):
    """Read-only parser for public anchors, headings, canonical URL and links."""
    def __init__(self, html):
        super().__init__(convert_charrefs=True)
        self.html = html
        self.ids = []
        self.hrefs = []
        self.canonical = None
        self.heading2 = []
        self._visible = []
        self._hidden = 0
        self._in_h2 = False
        self._h2_parts = []
        self.feed(html)
        self.close()

    @property
    def duplicate_ids(self):
        return sorted({value for value in self.ids if self.ids.count(value) > 1})

    @property
    def visible_text(self):
        return ' '.join(self._visible)

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if tag in ('script', 'style'):
            self._hidden += 1
        if self._hidden:
            return
        if attrs.get('id'):
            self.ids.append(attrs['id'])
        if tag == 'a' and attrs.get('href'):
            self.hrefs.append(attrs['href'])
        if tag == 'link' and attrs.get('rel') == 'canonical':
            self.canonical = attrs.get('href')
        if tag == 'h2':
            self._in_h2 = True
            self._h2_parts = []

    def handle_endtag(self, tag):
        if tag in ('script', 'style'):
            self._hidden = max(0, self._hidden - 1)
        if tag == 'h2' and self._in_h2:
            self.heading2.append(''.join(self._h2_parts).strip())
            self._in_h2 = False

    def handle_data(self, data):
        if not self._hidden:
            self._visible.append(data)
            if self._in_h2:
                self._h2_parts.append(data)


def fragment_errors(html):
    doc = Document(html)
    return [href[1:] for href in doc.hrefs if href.startswith('#') and
            href != '#' and href[1:] not in doc.ids]


class DocsFAQTests(unittest.TestCase):
    def test_negative_fixture_detects_broken_link(self):
        self.assertEqual(fragment_errors('<a href="#gone">Go</a><h2 id="still">Title</h2>'), ['gone'])

    def test_negative_fixture_detects_duplicate_ids(self):
        doc = Document('<h1 id="same">A</h1><h2 id="same">B</h2>')
        self.assertEqual(doc.duplicate_ids, ['same'])

    def test_faq_pages_keep_canonical_and_version_free_answers(self):
        files = sorted(FAQ_ROOT.glob('**/index.html'))
        found = {p.parent.name if p.parent != FAQ_ROOT else '' for p in files}
        self.assertTrue(CORE_SLUGS <= found, f'Missing permanent slugs: {CORE_SLUGS - found}')
        for page in files:
            with self.subTest(page=str(page.relative_to(SITE))):
                doc = Document(page.read_text(encoding='utf-8'))
                self.assertFalse(doc.duplicate_ids, doc.duplicate_ids)
                self.assertFalse(fragment_errors(doc.html), 'Broken same-page #fragment')
                self.assertFalse(VERSION_RE.search(doc.visible_text), 'Version leaked into public FAQ text')
                public_url = '/' + page.relative_to(SITE).as_posix().removesuffix('index.html')
                self.assertEqual(doc.canonical, 'https://docs.brovibe.cloud' + public_url)
                self.assertTrue(doc.ids, 'FAQ page has no IDs')
                if page != FAQ_ROOT / 'index.html':
                    self.assertGreaterEqual(len(doc.heading2), 1, 'FAQ has no questions')

    def test_faq_links_are_local_and_resolve(self):
        for page in sorted(FAQ_ROOT.glob('**/index.html')):
            doc = Document(page.read_text(encoding='utf-8'))
            for href in doc.hrefs:
                url = urlsplit(href)
                if url.scheme or url.netloc or not href or href == '#':
                    continue
                rel = unquote(url.path)
                target_base = SITE / rel.lstrip('/') if rel.startswith('/') else page.parent / rel
                target = target_base / 'index.html' if rel.endswith('/') else target_base if rel else page
                self.assertTrue(target.resolve().is_relative_to(SITE.resolve()), 'Link escapes site root')
                with self.subTest(page=str(page.relative_to(SITE)), href=href):
                    self.assertTrue(target.is_file(), f'Broken internal href: {href}')
                    if url.fragment:
                        self.assertIn(unquote(url.fragment), Document(target.read_text(encoding='utf-8')).ids,
                                      f'Missing target #{url.fragment} at {href}')

    def test_legacy_fragments_are_preserved(self):
        for rel_path, fragments in LEGACY_ANCHORS.items():
            with self.subTest(page=rel_path):
                page = SITE / rel_path
                self.assertTrue(page.is_file())
                doc = Document(page.read_text(encoding='utf-8'))
                self.assertFalse(set(fragments.split()) - set(doc.ids),
                                 f'Removed published anchors: {set(fragments.split()) - set(doc.ids)}')

    def test_search_has_every_faq_url_once(self):
        site_js = (SITE / 'site.js').read_text(encoding='utf-8')
        match = re.search(r'\bconst DOCS_SEARCH_INDEX\s*=\s*(\[[^\n]*\]);', site_js)
        self.assertIsNotNone(match, 'Unable to parse search index')
        entries = json.loads(match.group(1))
        urls = [entry['url'] for entry in entries]
        self.assertEqual(len(urls), len(set(urls)), 'Duplicate search URL')
        for page in sorted(FAQ_ROOT.glob('**/index.html')):
            with self.subTest(page=str(page.relative_to(SITE))):
                url = '/' + page.relative_to(SITE).as_posix().removesuffix('index.html')
                self.assertIn(url, urls, f'Not indexed in site.js: {url}')
                if url != '/broray/faq/':
                    self.assertIn('href="' + url + '"', (FAQ_ROOT / 'index.html').read_text(encoding='utf-8'))

    def test_deployment_manifest_is_exact_and_valid(self):
        deploy = (SITE / 'deploy.sh').read_text(encoding='utf-8')
        match = re.search(r'^FILES="([^"]+)"', deploy, re.M)
        self.assertIsNotNone(match, 'Cannot read deploy allowlist')
        allowed = match.group(1).split()
        self.assertEqual(len(allowed), len(set(allowed)), 'Duplicate deploy file')
        manifest = (SITE / 'SHA256SUMS').read_text(encoding='utf-8').splitlines()
        pairs = [line.split() for line in manifest]
        self.assertTrue(all(len(parts) == 2 for parts in pairs), 'Malformed checksum line')
        paths = [parts[1] for parts in pairs]
        self.assertEqual(len(paths), len(set(paths)), 'Duplicate checksum path')
        self.assertEqual(set(allowed), set(paths), 'Deploy allowlist differs from SHA256SUMS')
        for digest, rel_path in pairs:
            with self.subTest(path=rel_path):
                file = SITE / rel_path
                self.assertTrue(file.is_file(), f'Missing deploy asset: {rel_path}')
                self.assertEqual(len(digest), 64)
                self.assertEqual(hashlib.sha256(file.read_bytes()).hexdigest(), digest)


if __name__ == '__main__':
    unittest.main()
