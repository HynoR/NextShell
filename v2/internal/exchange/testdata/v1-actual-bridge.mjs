// Executes FIRST-PARTY v1 TypeScript functions unchanged through Node 24 type stripping.
// Only crypto-js (used solely by the unrelated FinalShell decryptor) is a throwing stub;
// these tests NEVER call that decryptor. No competitor SSH-client source is used.
import {registerHooks} from 'node:module';
import {readFileSync, writeFileSync} from 'node:fs';
const serviceRoot = new URL('../../../../apps/desktop/src/main/services/', import.meta.url);
registerHooks({resolve(specifier, context, nextResolve) {
  if (specifier === 'crypto-js') return {url:'data:text/javascript,export default new Proxy({}, {get(){throw new Error("unused FinalShell dependency called in NextShell-only test")}})',shortCircuit:true};
  if (specifier.startsWith('.') && context.parentURL?.startsWith(serviceRoot.href) && !specifier.endsWith('.ts')) return nextResolve(new URL(specifier+'.ts',context.parentURL).href,context);
  return nextResolve(specifier,context);
}});
const crypto = await import(new URL('connection-export-crypto.ts',serviceRoot));
const parser = await import(new URL('import-export.ts',serviceRoot));
if(process.argv[2]==='generate') {
 const file={format:'nextshell-connections',version:1,exportedAt:'2026-11-11T00:00:00.000Z',connections:[{name:'实际 TS 向量',host:'example.test',port:2222,username:'用户',authType:'password',password:'密码 🔑',keepAliveEnabled:false,keepAliveIntervalSec:45,groupPath:'/workspace/old-slug/生产/server',tags:['运维'],notes:'<notes>&',favorite:true,terminalEncoding:'gb18030',backspaceMode:'ascii-delete',deleteMode:'ascii-delete',monitorSession:false}]};
 const plain=JSON.stringify(file,null,2);const password='互通口令 🔒';const payload=await crypto.encryptConnectionExportPayload(plain,password);
 writeFileSync(new URL('v1-actual-vector.json',import.meta.url),JSON.stringify({source:'apps/desktop/src/main/services/connection-export-crypto.ts + import-export.ts',password,plain,payload,parsed:parser.parseNextShellImport(file)},null,2)+'\n');
} else {
 const input=JSON.parse(readFileSync(0,'utf8'));let raw=input.raw.trim();
 if(raw.startsWith('b64##')) raw=await crypto.decryptConnectionExportPayload(raw.slice(5),input.password);
 const file=JSON.parse(raw);if(!parser.isNextShellFormat(file)||file.version!==1)throw new Error('wrong exchange format');
 process.stdout.write(JSON.stringify({plain:raw,entries:parser.parseNextShellImport(file)}));
}
