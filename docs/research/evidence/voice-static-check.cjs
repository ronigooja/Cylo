// 研究证据；不连接上游，不运行网关。用法：node voice-static-check.cjs /tmp/cylo-research-voice
const fs = require('fs'), vm = require('vm'), cp = require('child_process'), os = require('os'), path = require('path');
const repo = process.argv[2];
const expected = '6338ac478070aaa34bfe7a3c0469caf9e9174c02';
if (cp.execFileSync('git', ['-C', repo, 'rev-parse', 'HEAD'], {encoding:'utf8'}).trim() !== expected) throw Error('unexpected source commit');
const html = fs.readFileSync(path.join(repo,'static/voice.html'),'utf8');
const dir = fs.mkdtempSync(path.join(os.tmpdir(),'cylo-voice-check-'));
const scripts = [...html.matchAll(/<script\b[^>]*>([\s\S]*?)<\/script>/g)];
scripts.forEach((m,i) => {const f=path.join(dir,`${i}.js`);fs.writeFileSync(f,m[1]);cp.execFileSync(process.execPath,['--check',f]);});
console.log(`PASS: ${scripts.length} inline scripts pass node --check`);
// 固定commit函数包含简单花括号，按已知下一个函数边界提取，避免执行完整DOM代码。
const start=html.indexOf('  function interruptAssistant('), end=html.indexOf('  function evaluateAutoInterrupt(',start);
if(start<0||end<0) throw Error('function boundary missing');
const events=[];
const c={inCall:true,assistantSpeaking:true,awaitingInterruptAck:false,dcChannel:{readyState:'open'},muted:false,Date,autoInterruptStartedAt:1,autoInterruptCooldownUntil:0,AUTO_INTERRUPT_COOLDOWN_MS:500,remoteSpeakingUntil:1,setUIKey(){},sendDcEvent(e){events.push(e)},log(){}};
vm.createContext(c);vm.runInContext(html.slice(start,end),c);
if(c.interruptAssistant()!==true||events.length!==1||events[0].payload.action!=='stop_speaking')throw Error('active interruption');
if(c.interruptAssistant()!==false||events.length!==1)throw Error('duplicate interruption');
c.awaitingInterruptAck=false;c.assistantSpeaking=true;c.muted=true;
if(c.interruptAssistant()!==false)throw Error('muted guard');
if(c.interruptAssistant({requireUnmuted:false})!==true||events.length!==2)throw Error('text interruption');
console.log('PASS: active stop_speaking, duplicate suppression, muted mic guard, text interruption');
console.log('NOT TESTED: DOM, WebRTC, audio, upstream acknowledgement, Chinese recognition, mobile devices');
fs.rmSync(dir,{recursive:true,force:true});
