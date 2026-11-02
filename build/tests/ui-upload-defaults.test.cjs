const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');

function uploadDefaultsFixture(saved = {}) {
    const fields = Object.fromEntries([
        'allowedDownloads', 'expiryDays', 'password',
        'enableDownloadLimit', 'enableTimeLimit', 'enablePassword'
    ].map(id => [id, {disabled: false}]));
    const context = vm.createContext({
        document: {getElementById: id => fields[id]},
        localStorage: {getItem: key => saved[key] ?? null},
        Date: {now: () => 1_800_000_000_000}
    });
    vm.runInContext(fs.readFileSync(path.join(__dirname,
        '../../internal/webserver/web/static/js/admin_ui_upload.js'), 'utf8'), context);
    return {context, fields};
}

test('new upload preferences default to a five-day time limit', () => {
    const {context, fields} = uploadDefaultsFixture();
    context.setUploadDefaults();
    assert.equal(fields.expiryDays.value, 5);
    assert.equal(fields.expiryDays.disabled, false);
    assert.equal(fields.enableTimeLimit.checked, true);
});

test('saved upload duration and unlimited-time preferences remain unchanged', () => {
    const {context, fields} = uploadDefaultsFixture({defaultExpiry: '14'});
    context.setUploadDefaults();
    assert.equal(fields.expiryDays.value, '14');
    assert.equal(fields.enableTimeLimit.checked, true);

    const unlimited = uploadDefaultsFixture({defaultExpiry: '9', defaultUnlimitedTime: 'true'});
    unlimited.context.setUploadDefaults();
    assert.equal(unlimited.fields.expiryDays.value, '9');
    assert.equal(unlimited.fields.expiryDays.disabled, true);
    assert.equal(unlimited.fields.enableTimeLimit.checked, false);
});

test('editing an unlimited file suggests five days only for a past timestamp', () => {
    const {context} = uploadDefaultsFixture();
    const now = 1_800_000_000;
    assert.equal(context.defaultExpiryIfPast(now - 1), now + 5 * 24 * 60 * 60);
    assert.equal(context.defaultExpiryIfPast(now + 14 * 24 * 60 * 60), now + 14 * 24 * 60 * 60);
});
