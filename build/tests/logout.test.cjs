const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
function fixture(fetch) {
 const events = [], errors = [];
 const context = vm.createContext({
  ClipboardJS: function(){this.on=()=>{};}, document:{}, URLSearchParams,
  window:{location:{href:'./admin'}}, sseWorkerPort:{postMessage:m=>events.push(m)},
  fetch, console:{error(){}}, alert:m=>errors.push(m), t:k=>k
 });
 vm.runInContext(fs.readFileSync(path.join(__dirname,'../../internal/webserver/web/static/js/admin_ui_allPages.js'),'utf8'),context);
 return {context,events,errors};
}
test('logout uses fresh token then POST, broadcasts only after confirmation',async()=>{
 const requests=[];
 const {context,events,errors}=fixture(async(url,options)=>{
  requests.push({url,options});
  assert.equal(events.length,0);
  return {ok:true,json:async()=>url==='./logoutToken'?{token:'fresh-token'}:{redirect:'login?consent=true'}};
 });
 await context.doLogout();
 assert.equal(requests.length,2);
 assert.equal(requests[0].url,'./logoutToken');
 assert.equal(requests[1].url,'./logout');
 assert.equal(requests[1].options.method,'POST');
 assert.equal(requests[1].options.body.get('csrf-token'),'fresh-token');
 assert.equal(events.length,1);
 assert.equal(context.window.location.href,'./login?consent=true');
 assert.equal(errors.length,0);
});
test('rejected logout keeps other tabs active and allows fresh retry',async()=>{
 let calls=0;
 const {context,events,errors}=fixture(async()=>{
  calls++;
  return calls%2===1?{ok:true,json:async()=>({token:'fresh-'+calls})}:{ok:false};
 });
 await context.doLogout(); await context.doLogout();
 assert.equal(calls,4); assert.equal(events.length,0); assert.equal(errors.length,2);
 assert.equal(context.window.location.href,'./admin');
});
test('repeated clicks share one in-flight logout',async()=>{
 let release,calls=0;
 const {context,events}=fixture(async()=>{
  calls++;
  if(calls===1) await new Promise(resolve=>release=resolve);
  return {ok:true,json:async()=>calls===1?{token:'fresh'}:{redirect:'login'}};
 });
 const pending=context.doLogout(); await context.doLogout();
 assert.equal(calls,1);release();await pending;assert.equal(calls,2);assert.equal(events.length,1);
});
