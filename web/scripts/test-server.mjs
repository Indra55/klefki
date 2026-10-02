import {mkdtemp,rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {spawn,execFileSync} from 'node:child_process';
const dir=await mkdtemp(join(tmpdir(),'vault-web-server-'));
execFileSync('go',['build','-o',join(dir,'server'),'../cmd/vault-server'],{stdio:'inherit'});
const child=spawn(join(dir,'server'),['-listen','127.0.0.1:5188','-dev-http','-data',join(dir,'data'),'-web','dist'],{stdio:'ignore'});
async function stop(){child.kill('SIGTERM');}
process.on('SIGTERM',stop);process.on('SIGINT',stop);
child.on('exit',async code=>{await rm(dir,{recursive:true,force:true});process.exit(code||0);});
