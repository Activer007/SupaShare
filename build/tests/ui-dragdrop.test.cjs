const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const scripts = path.join(__dirname, '../../internal/webserver/web/static/js');

function fixture() {
    const observers = [];
    function changed(target, attributeName) {
        for (const observer of observers) {
            if (observer.targets.some(entry => entry.target === target &&
                entry.options.attributeFilter.includes(attributeName))) {
                queueMicrotask(() => observer.callback());
            }
        }
    }
    function target() {
        const listeners = new Map();
        return {
            addEventListener(type, callback, capture) {
                const entries = listeners.get(type) || [];
                entries.push({callback, capture}); listeners.set(type, entries);
            },
            dispatch(type, event = {}) {
                for (const entry of listeners.get(type) || []) entry.callback(event);
            }
        };
    }
    function node(text = '') {
        let value = text, disabled = false;
        const classes = new Set();
        const element = {writes: 0,
            get textContent() {return value;},
            set textContent(next) {value = next; this.writes++;},
            get disabled() {return disabled;},
            set disabled(next) {disabled = next; changed(element, 'disabled');},
            classList: {
                add(name) {classes.add(name); changed(element, 'class');},
                remove(name) {classes.delete(name); changed(element, 'class');},
                contains(name) {return classes.has(name);}
            }
        };
        return element;
    }
    const zone = node(), title = node('Drag files here'), subtitle = node('Choose or paste files');
    const trigger = node(), document = target(), window = target();
    document.getElementById = id => ({selectUploadFiles:trigger, uploadBoxTitle:title,
        uploadBoxSubtitle:subtitle}[id]);
    const context = vm.createContext({document, window, t:key=>key, Dropzone:{options:{}},
        MutationObserver:class {
            constructor(callback) {this.callback = callback; this.targets = []; observers.push(this);}
            observe(target, options) {this.targets.push({target, options});}
        }});
    vm.runInContext(fs.readFileSync(path.join(scripts, 'admin_ui_dragdrop.js'), 'utf8'), context);
    const dropzone = {element:zone, disabled:false, on(){}};
    const feedback = context.createUploadDragFeedback(dropzone);
    return {context, zone, title, subtitle, trigger, dropzone, feedback, document, window,
        active:()=>zone.classList.contains('is-file-dragover')};
}

const fileDrag = () => ({dataTransfer:{types:['Files'], files:[], dropEffect:'none'}});

test('protected OS file drags activate once and dragover requests a copy without rewriting copy', () => {
    const f = fixture(), event = fileDrag();
    f.feedback.enter(event);
    assert.equal(f.active(), true);
    assert.equal(f.title.textContent, 'files.dropToUpload');
    assert.equal(f.subtitle.textContent, 'files.dropUsesRules');
    const writes = [f.title.writes, f.subtitle.writes];
    f.feedback.over(event); f.feedback.over(event);
    assert.equal(event.dataTransfer.dropEffect, 'copy');
    assert.deepEqual([f.title.writes, f.subtitle.writes], writes);
});

test('text, link and missing data transfers never advertise a file upload', () => {
    const f = fixture();
    for (const event of [{dataTransfer:{types:['text/plain']}},
        {dataTransfer:{types:['text/uri-list']}}, {}]) {
        f.feedback.enter(event); f.feedback.over(event);
        assert.equal(f.active(), false);
        assert.equal(f.title.textContent, 'Drag files here');
    }
    f.feedback.enter(fileDrag());
    f.feedback.over({dataTransfer:{types:['text/plain']}});
    assert.equal(f.active(), false);
});

test('disabled upload controls and Dropzone cannot activate and availability changes clear feedback', async () => {
    const f = fixture();
    f.trigger.disabled = true; f.feedback.enter(fileDrag());
    assert.equal(f.active(), false);
    f.trigger.disabled = false; f.dropzone.disabled = true; f.feedback.over(fileDrag());
    assert.equal(f.active(), false);
    f.dropzone.disabled = false; f.feedback.enter(fileDrag());
    f.trigger.disabled = true;
    await Promise.resolve();
    assert.equal(f.active(), false);
    f.trigger.disabled = false; f.feedback.enter(fileDrag());
    // Dropzone.disable removes its clickable class before setting disabled.
    f.zone.classList.remove('dz-clickable'); f.dropzone.disabled = true;
    await Promise.resolve();
    assert.equal(f.active(), false);
    assert.equal(f.title.textContent, 'Drag files here');
});

test('nested artwork and text enter/leave events keep the area highlighted until its final leave', () => {
    const f = fixture();
    f.feedback.enter(fileDrag()); // area
    f.feedback.enter(fileDrag()); // child artwork
    f.feedback.leave();
    assert.equal(f.active(), true);
    f.feedback.enter(fileDrag()); // child text
    f.feedback.leave();
    assert.equal(f.active(), true);
    f.feedback.leave();
    assert.equal(f.active(), false);
    f.feedback.leave(); // a stale extra leave must not poison the next drag
    f.feedback.over(fileDrag());
    assert.equal(f.active(), true);
    f.feedback.leave();
    assert.equal(f.active(), false);
});

test('drop elsewhere, drag end, window blur and Escape restore the resting instructions', () => {
    const f = fixture();
    for (const [target, type, event] of [[f.document,'drop',{}], [f.document,'dragend',{}],
        [f.window,'blur',{}], [f.document,'keydown',{key:'Escape'}]]) {
        f.feedback.enter(fileDrag()); f.feedback.enter(fileDrag());
        target.dispatch(type, event);
        assert.equal(f.active(), false);
        assert.equal(f.title.textContent, 'Drag files here');
        assert.equal(f.subtitle.textContent, 'Choose or paste files');
    }
    f.feedback.enter(fileDrag()); f.document.dispatch('keydown', {key:'Enter'});
    assert.equal(f.active(), true);
});

test('instructions changed by E2E readiness are captured when the drag starts, not during initialization', () => {
    const f = fixture();
    f.trigger.disabled = true; f.title.textContent = 'Loading encryption';
    f.feedback.enter(fileDrag()); assert.equal(f.active(), false);
    f.title.textContent = 'Ready for encrypted files';
    f.subtitle.textContent = 'Keep the encryption key'; f.trigger.disabled = false;
    f.feedback.enter(fileDrag()); f.feedback.reset();
    assert.equal(f.title.textContent, 'Ready for encrypted files');
    assert.equal(f.subtitle.textContent, 'Keep the encryption key');
    f.title.textContent = 'Updated ready instructions';
    f.feedback.enter(fileDrag()); f.feedback.reset();
    assert.equal(f.title.textContent, 'Updated ready instructions');
});

test('Dropzone presentation callbacks use the feedback helper and leave its file admission hook intact', () => {
    const f = fixture();
    vm.runInContext(fs.readFileSync(path.join(scripts, 'admin_ui_upload.js'), 'utf8'), f.context);
    f.context.initDropzone();
    const options = f.context.Dropzone.options.uploaddropzone;
    options.init.call(f.dropzone);
    options.dragenter.call(f.dropzone, fileDrag()); assert.equal(f.active(), true);
    options.dragover.call(f.dropzone, fileDrag());
    options.dragleave.call(f.dropzone); assert.equal(f.active(), false);
    for (const callback of [options.drop, options.dragend]) {
        options.dragenter.call(f.dropzone, fileDrag()); callback.call(f.dropzone);
        assert.equal(f.active(), false);
    }
    const results = [];
    f.context.validateUploadRules = () => false;
    options.accept.call(f.dropzone, {}, result => results.push(result));
    f.context.validateUploadRules = () => true;
    options.accept.call(f.dropzone, {}, result => results.push(result));
    f.trigger.disabled = true;
    options.accept.call(f.dropzone, {}, result => results.push(result));
    assert.deepEqual(results, ['files.summaryInvalid', undefined, 'files.loadingE2E']);
});
