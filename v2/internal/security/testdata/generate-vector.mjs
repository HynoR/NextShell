// Derived only from NextShell 1.x packages/security/src/index.ts constants and byte layout.
// Deterministic test fixture. Never use a fixed salt or nonce for real encryption.
import {scryptSync,createCipheriv} from 'node:crypto';
import {writeFileSync} from 'node:fs';
const password='密码-correct-horse',plaintext='NextShell 中文 🔐',aad='nextshell-secret:test';
const salt=Buffer.from(Array.from({length:32},(_,i)=>i)),iv=Buffer.from(Array.from({length:12},(_,i)=>i+32));
const key=scryptSync(password,salt,32,{N:16384,r:8,p:1});
const cipher=createCipheriv('aes-256-gcm',key,iv,{authTagLength:16});cipher.setAAD(Buffer.from(aad));
const ciphertext=Buffer.concat([cipher.update(plaintext,'utf8'),cipher.final()]),tag=cipher.getAuthTag();
const b64=b=>b.toString('base64');
writeFileSync(new URL('./node-envelope.json',import.meta.url),JSON.stringify({password,plaintext,keyHex:key.toString('hex'),envelope:{v:1,alg:'aes-256-gcm',kdf:'scrypt',salt:b64(salt),iv:b64(iv),aad,ciphertext:b64(ciphertext),tag:b64(tag)}},null,2)+'\n');
