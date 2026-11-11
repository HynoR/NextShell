// Independent Node crypto known-answer vector for OUR new offline protocol, not v1 cloud.
import {scryptSync,createCipheriv} from 'node:crypto';
import {writeFileSync} from 'node:fs';
const domain='nextshell/cloud/v2/';const workspace='10000000-0000-4000-8000-000000000001';const keyId='20000000-0000-4000-8000-000000000001';const password='independent KEK 密码';
const salt=Buffer.from(Array.from({length:32},(_,i)=>i+32));const dek=Buffer.from(Array.from({length:32},(_,i)=>i));const nonce=Buffer.from(Array.from({length:12},(_,i)=>i+64));
const kdf={name:'scrypt',n:32768,r:8,p:1,salt:salt.toString('base64')};
const kek=scryptSync(password,Buffer.from(JSON.stringify([domain+'kek',2,workspace,keyId,kdf.salt])),32,{N:kdf.n,r:kdf.r,p:kdf.p,maxmem:64*1024*1024});
function seal(key,nonce,plain,aad){const c=createCipheriv('aes-256-gcm',key,nonce);c.setAAD(Buffer.from(JSON.stringify(aad)));return Buffer.concat([c.update(plain),c.final(),c.getAuthTag()]);}
const wrapped={version:2,workspaceId:workspace,keyId,kdf,nonce:nonce.toString('base64'),ciphertext:seal(kek,nonce,dek,[domain+'wrap-dek',2,workspace,keyId,kdf]).toString('base64')};
const snapshot={version:2,workspaceId:workspace,name:'vector-workspace',records:[]};const n2=Buffer.from(Array.from({length:12},(_,i)=>i+80));const id='30000000-0000-4000-8000-000000000001';
const commit={version:2,workspaceId:workspace,keyId,commitId:id,parentId:'',nonce:n2.toString('base64'),ciphertext:seal(dek,n2,Buffer.from(JSON.stringify(snapshot)),[domain+'snapshot',2,workspace,keyId,id,'']).toString('base64')};
writeFileSync(new URL('node-vector.json',import.meta.url),JSON.stringify({password,dek:dek.toString('base64'),kek:kek.toString('base64'),wrapped,snapshot,commit},null,2)+'\n');
