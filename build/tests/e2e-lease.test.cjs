const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
const path=require('node:path');
const source=fs.readFileSync(path.join(__dirname,'../../internal/webserver/web/static/js/admin_api.js'),'utf8');
function setup(mode) {
 const calls=[];let auth=0;
 const context=vm.createContext({console:{error(){}},t:x=>x,Map,Date,Error,JSON,AbortController,setTimeout,clearTimeout,fetch:async(url,options)=>{
  calls.push({url,headers:options.headers});
  if(url.endsWith('/lock')) return {ok:mode!=='busy',status:409,json:async()=>({leaseToken:'transaction-token'}),text:async()=> 'busy'};
  if(mode==='read-failed'&&url.endsWith('/get')) return {ok:false,status:500,text:async()=> 'failed'};
  return {ok:true,text:async()=> '{}'};
 }});
 vm.runInContext(source,context);
 context.getToken=async()=>`auth-${++auth}`;
 return {context,calls};
}
test('busy acquisition does not invoke operation or unlock',async()=>{
 const {context,calls}=setup('busy');let invoked=false;
 await assert.rejects(context.withE2eLease(async()=>{invoked=true;}),/busy/);
 assert.equal(invoked,false);assert.equal(calls.length,4);
});
test('failed read releases only its acquired lease',async()=>{
 const {context,calls}=setup('read-failed');
 await assert.rejects(context.withE2eLease(token=>context.apiE2eGet(token)),/failed/);
 assert.equal(calls.length,3);
 assert.equal(calls[2].headers.e2eLease,'transaction-token');
 assert.ok(calls[2].url.endsWith('/unlock'));
});
test('read-modify-write shares local lease even when API token rotates',async()=>{
 const {context,calls}=setup();
 await context.withE2eLease(async token=>{await context.apiE2eGet(token);await context.apiE2eStore('merged',token);});
 assert.equal(calls.length,4);
 for(const call of calls.slice(1)) assert.equal(call.headers.e2eLease,'transaction-token');
 assert.equal(new Set(calls.map(c=>c.headers.apikey)).size,4);
 await assert.rejects(context.apiE2eStore('stale'),/Missing E2E lease/);
 assert.equal(calls.length,4);
});
test('setup and upload callers use transaction helper, not unconditional unlock',()=>{
 for(const file of ['internal/webserver/web/templates/html_end2end.tmpl','internal/webserver/web/static/js/admin_ui_upload.js','internal/webserver/web/static/js/end2end_admin.js']) {
  const text=fs.readFileSync(path.join(__dirname,'../..',file),'utf8');
  assert.match(text,/withE2eLease/);assert.doesNotMatch(text,/apiE2eMutexLockUnlock/);
 }
});

test('admission retries busy only; callback failure is not replayed',async()=>{
 const {context,calls}=setup(); let attempts=0, operations=0;
 context.fetch=async(url,options)=>{
  calls.push({url,headers:options.headers});
  if(url.endsWith('/lock') && ++attempts<3) return {ok:false,status:429,text:async()=> 'busy'};
  return {ok:true,json:async()=>({leaseToken:'mine'}),text:async()=> '{}'};
 };
 await assert.rejects(context.withE2eLease(async()=>{operations++;throw new Error('set failed');}),/set failed/);
 assert.equal(attempts,3);assert.equal(operations,1);
 assert.equal(calls.filter(c=>c.url.endsWith('/unlock')).length,1);
});
test('cancel during retry delay stops admission without unlock',async()=>{
 const {context,calls}=setup('busy'); const controller=new AbortController();
 const task=context.withE2eLease(()=>assert.fail('callback'),{signal:controller.signal});
 setTimeout(()=>controller.abort(new Error('cancelled')),20);
 await assert.rejects(task,/cancelled/);
 assert.equal(calls.length,1);
});
test('cancel in flight propagates to fetch and never unlocks',async()=>{
 const {context}=setup();let calls=0;
 context.fetch=async(url,{signal})=>{calls++;return new Promise((resolve,reject)=>{
  signal.addEventListener('abort',()=>reject(signal.reason),{once:true});
 });};
 const controller=new AbortController();
 const task=context.withE2eLease(()=>assert.fail('callback'),{signal:controller.signal});
 setTimeout(()=>controller.abort(new Error('cancelled')),20);
 await assert.rejects(task,/cancelled/); assert.equal(calls,1);
});
test('same-user concurrent metadata transactions retry admission and merge fresh state',async()=>{
 const {context}=setup();let owner=null,serial=0,busy=0;const metadata=[];
 context.fetch=async(url,{headers})=>{
  if(url.endsWith('/lock')){
   if(owner){busy++;return {ok:false,status:409,text:async()=> 'busy'};}
   owner='lease-'+(++serial);
   return {ok:true,json:async()=>({leaseToken:owner})};
  }
  assert.equal(headers.e2eLease,owner);
  owner=null;return {ok:true,text:async()=> '{}'};
 };
 const transaction=id=>context.withE2eLease(async()=>{
  const fresh=[...metadata];
  await new Promise(resolve=>setTimeout(resolve,30));
  metadata.splice(0,metadata.length,...fresh,id);
 });
 await Promise.all([transaction('a'),transaction('b'),transaction('c')]);
 assert.equal(busy>0,true);assert.deepEqual([...metadata].sort(),['a','b','c']);assert.equal(serial,3);
});
