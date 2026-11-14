const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const root = path.resolve(__dirname, '../..');
const read = p => fs.readFileSync(path.join(root, p), 'utf8');
const version = read('cmd/supashare/Main.go').match(/const versionGokapi = "([^"]+)"/)[1];
const [major, minor, patch] = version.split('.').map(Number);
const versionInt = major * 10000 + minor * 100 + patch;

test('source, API, OpenAPI and documentation agree on the product version', () => {
  const api = read('internal/webserver/api/VersionNumbers.go');
  assert.ok(api.includes(`const versionReadable = "${version}"`));
  assert.ok(api.includes(`const versionInt = ${versionInt}`));
  const specification = JSON.parse(read('openapi.json'));
  assert.equal(specification.info.version, version);
  assert.deepEqual(JSON.parse(read('internal/webserver/web/static/apidocumentation/openapi.json')), specification);
  assert.ok(read('docs/conf.py').includes(`release = '${version}'`));
  assert.ok(read('README.md').includes(`**源码版本 ${version}**`));
  assert.ok(read('README.en.md').includes(`**Source version ${version}**`));
  assert.ok(read('docs/changelog.rst').includes(`v${version} (`));
});

test('generated UI version and cache-busted assets exist', () => {
  const template = read('internal/webserver/web/templates/string_constants.tmpl');
  assert.ok(template.includes(`{{define "version"}}${version}{{end}}`));
  assert.ok(template.includes(`{{define "version_major"}}${major}{{end}}`));
  const value = key => template.match(new RegExp(`\\{\\{define "${key}"\\}\\}([^{}]+)\\{\\{end\\}\\}`))[1];
  for (const [key, assets] of [
    ['js_admin_version', ['js/min/admin.min.%s.js']],
    ['js_e2eversion', ['js/min/end2end_admin.min.%s.js', 'js/min/end2end_download.min.%s.js']],
    ['css_main', ['css/min/gokapi.min.%s.css']],
  ]) {
    for (const asset of assets) {
      assert.ok(fs.existsSync(path.join(root, 'internal/webserver/web/static', asset.replace('%s', value(key)))));
    }
  }
});
