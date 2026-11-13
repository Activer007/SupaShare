const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const root = path.join(__dirname, '../../internal/webserver/web');
function load(file, secure, origin='https://share.test', transferable=false) {
  const frames=[], messages=[], downloads=[], channels=[], blobs=[];
  const document={documentElement:{style:{}}, body:{appendChild(e){frames.push(e)}},
    createElement(){return {click(){downloads.push(this.href)},addEventListener(name,fn){fn()},contentWindow:{postMessage(...args){messages.push(args)}}}}};
  const LocalURL = class extends URL {
    static createObjectURL(blob) { blobs.push(blob); return 'blob:synthetic'; }
    static revokeObjectURL() {}
  };
  const context={module:{exports:{}}, console, URL:LocalURL, Uint8Array, Blob, Response, ReadableStream, WritableStream,
    location:{origin,href:origin+'/share/download'}, document, navigator:{serviceWorker:{}},
    MessageChannel:class{constructor(){channels.push(this);this.port1={sent:[],postMessage(value){this.sent.push(value)},close(){this.closed=true}};this.port2={close(){this.closed=true}}}},
    setTimeout(){},window:{HTMLElement:function HTMLElement(){},isSecureContext:secure,WritableStream}};
  if (transferable) context.TransformStream = TransformStream;
  vm.runInNewContext(fs.readFileSync(path.join(root,file),'utf8'),context);
  return {saver:context.module.exports,frames,messages,downloads,channels,blobs};
}
for(const file of ['static/js/streamsaver.js','static/js/min/streamsaver.min.js']) {
  for (const transferable of [false, true]) {
    test(file + ' invalid helper reply errors the writer, transferable=' + transferable, async () => {
      for (const download of ['https://outside.test/steal', 'http://[']) {
        const {saver, frames, channels} = load(file, true, 'https://share.test', transferable);
        const writer = saver.createWriteStream('synthetic.txt').getWriter();
        const outcome = writer.closed.then(() => 'closed', error => error.message);
        writer.close().catch(() => {});
        channels.at(-1).port1.onmessage({data: {download}});
        const result = await Promise.race([outcome, new Promise(resolve => setTimeout(() => resolve('pending'), 50))]);
        assert.match(result, /Invalid download URL/);
        await assert.rejects(writer.write(new Uint8Array([1])), /Invalid download URL/);
        assert.equal(channels.at(-1).port1.closed, true);
        assert.equal(channels.at(-1).port2.closed, true);
        assert.equal(frames.length, 1);
      }
    });
    test(file + ' abort clears channels and a later stream succeeds, transferable=' + transferable, async () => {
      const {saver, channels} = load(file, true, 'https://share.test', transferable);
      const writer = saver.createWriteStream('cancel.txt').getWriter();
      writer.closed.catch(() => {});
      await writer.abort(new Error('synthetic cancellation'));
      assert.equal(channels.at(-1).port1.closed, true);
      assert.equal(channels.at(-1).port2.closed, true);
      const rejected = saver.createWriteStream('helper-abort.txt').getWriter();
      const helperAborted = assert.rejects(rejected.closed, /Download aborted by helper/);
      channels.at(-1).port1.onmessage({data: {abort: true}});
      await helperAborted;
      assert.equal(channels.at(-1).port1.closed, true);
      assert.equal(channels.at(-1).port2.closed, true);
      const retry = saver.createWriteStream('retry.txt').getWriter();
      const received = [];
      const drain = transferable ? channels.at(-1).port1.sent[0].readableStream.pipeTo(new WritableStream({
        write(chunk) { received.push(...chunk); }
      })) : Promise.resolve();
      await retry.write(new Uint8Array([1, 2, 3]));
      let complete = false;
      const closing = retry.close().then(() => { complete = true; });
      await new Promise(resolve => setImmediate(resolve));
      assert.equal(complete, false);
      channels.at(-1).port1.onmessage({data: {download: 'https://share.test/serviceworker/retry'}});
      await closing;
      await drain;
      if (transferable) assert.deepEqual(received, [1, 2, 3]);
      assert.equal(channels.at(-1).port1.closed, true);
      assert.equal(channels.at(-1).port2.closed, true);
    });
  }
  test(file+' remote HTTP saves locally without helper',async()=>{
    const {saver,frames,messages,downloads}=load(file,false,'http://remote.test');
    const writer=saver.createWriteStream('test').getWriter();
    await writer.write(new Uint8Array([1,2,3]));await writer.close();
    assert.equal(frames.length,0);assert.equal(messages.length,0);
    assert.match(downloads[0],/^blob:/);
  });
  test(file + ' remote HTTP saves exactly 64 MiB locally', async () => {
    const {saver, blobs, frames} = load(file, false, 'http://remote.test');
    const writer = saver.createWriteStream('boundary.bin').getWriter();
    await writer.write(new Uint8Array(64 * 1024 * 1024));
    await writer.close();
    assert.equal(blobs[0].size, 64 * 1024 * 1024);
    assert.equal(frames.length, 0);
  });
  test(file+' remote HTTP rejects byte 64MiB+1 and does not save partial data',async()=>{
    const {saver,frames,downloads}=load(file,false,'http://remote.test');
    const writer=saver.createWriteStream('test').getWriter();
    await writer.write(new Uint8Array(64*1024*1024));
    await assert.rejects(writer.write(new Uint8Array([1])),/64 MiB/);
    await assert.rejects(writer.close());
    assert.equal(frames.length,0);assert.equal(downloads.length,0);
  });
  test(file+' HTTP abort is safe without a channel',async()=>{
    const {saver,downloads}=load(file,false,'http://remote.test');
    const writer=saver.createWriteStream('test').getWriter();
    await writer.write(new Uint8Array([1]));await writer.abort();
    assert.equal(downloads.length,0);
  });
  test(file+' refuses external helper even in secure context',()=>{
    const {saver,frames}=load(file,true); saver.mitm='https://outside.test/helper';
    assert.throws(()=>saver.createWriteStream('test'),/Cross-origin/);assert.equal(frames.length,0);
  });
  test(file+' refuses external service-worker download URL',()=>{
    const {saver,frames,channels}=load(file,true);
    saver.createWriteStream('test');
    channels.at(-1).port1.onmessage({data:{download:'https://outside.test/steal'}});
    assert.equal(frames.length,1);
  });
  for(const origin of ['https://share.test','http://localhost:8080']) test(file+' uses exact same origin '+origin,()=>{
    const {saver,frames,messages}=load(file,true,origin); saver.createWriteStream('test');
    assert.equal(new URL(frames[0].src).origin,origin);assert.equal(messages[0][1],origin);
  });
}
test('helper drops messages from wrong origin/source before and after worker readiness',async()=>{
  const source=fs.readFileSync(path.join(root,'static/serviceworker/index.html'),'utf8').match(/<script>([\s\S]*)<\/script>/)[1];
  const parent={},sent=[];const sw={postMessage(...args){sent.push(args)}};
  const context={window:{parent,opener:null}, location:{origin:'https://share.test',href:'https://share.test/serviceworker/index.html',search:''},
    navigator:{serviceWorker:{getRegistration:async()=>({active:sw,scope:'https://share.test/serviceworker/'})}},
    URL,URLSearchParams,Headers,document:{referrer:''},console,setInterval(){}};
  vm.runInNewContext(source,context);
  const event=(origin,source)=>({origin,source,ports:[{}],data:{headers:{},pathname:'x',transferringReadable:true}});
  context.window.onmessage(event('https://evil.test',parent));
  context.window.onmessage(event('https://share.test',{}));
  context.window.onmessage(event('https://share.test',parent));
  await new Promise(resolve=>setImmediate(resolve));
  assert.equal(sent.length,1);
  context.window.onmessage(event('https://evil.test',parent));
  context.window.onmessage(event('https://share.test',{}));
  assert.equal(sent.length,1);
  context.window.onmessage(event('https://share.test',parent));assert.equal(sent.length,2);
});
test('download warns about development HTTP limits; backup uses local Blob',()=>{
 const template=fs.readFileSync(path.join(root,'templates/html_download.tmpl'),'utf8');
 assert.match(template,/download.httpWarning/);assert.match(template,/download.httpLimit/);
 assert.match(template,/reader\?\.cancel/);
 assert.doesNotMatch(template,/forceu.github.io|UsesHttps/);
 const backup=fs.readFileSync(path.join(root,'templates/html_end2end.tmpl'),'utf8');
 assert.doesNotMatch(backup,/streamSaver|streamsaver/);assert.match(backup,/URL.createObjectURL\(new Blob/);assert.match(backup,/URL.revokeObjectURL/);
});
