import {StrictMode} from 'react';
import {afterEach,beforeEach,describe,expect,it,vi} from 'vitest';
import {act,cleanup,render,screen,waitFor,within} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import App from './App';
import {visibleItems,type Item,type State} from './api';
vi.mock('./Preview',()=>({default:()=>null}));
const item:Item={object:'object',revision:'revision',name:'Passport.txt',type:'file',parent:null,tags:['Identity'],favorite:false,size:24,conflict:false,trashed:false,trashRoot:false};
let state:State;let events:Record<string,(e:any)=>void>;
beforeEach(()=>{state={config:{accountPath:'account.v1',storePath:'local.db',server:'',preferences:{theme:'dark',idleMinutes:5}},unlocked:true,generation:1,accountID:'account',recoveryOnly:false,sync:'Local only',pending:0,lastSync:''};events={};vi.spyOn(document,'hasFocus').mockReturnValue(true);window.matchMedia=vi.fn().mockReturnValue({matches:true,addEventListener:vi.fn(),removeEventListener:vi.fn()});window.runtime={EventsOn:(name,cb)=>{events[name]=cb;return()=>{delete events[name]}}};window.go={main:{App:{State:vi.fn(async()=>state),List:vi.fn(async()=>({generation:state.generation,items:[item]})),Touch:vi.fn(),Lock:vi.fn(async()=>{state={...state,unlocked:false,generation:2};events['klefki:event']?.({generation:2,kind:'locked'})}),Edit:vi.fn(async()=>''),ClosePreview:vi.fn()} as any}}});
afterEach(()=>{cleanup();vi.restoreAllMocks()});
describe('library',()=>{
 it('creates a folder with an emoji and changes or resets its icon in details',async()=>{
  const bridge=window.go!.main.App;
  let folder:Item={...item,object:'folder',revision:'folder-rev',type:'folder',name:'Study',tags:[]};
  bridge.List=vi.fn(async()=>({generation:1,items:[folder]}));
  bridge.FolderWithIcon=vi.fn(async(name,parent,icon)=>{folder={...folder,name,parent,folder_icon:icon};return folder.revision});
  bridge.Edit=vi.fn(async(revision,edit)=>{folder={...folder,...edit};return revision});
  render(<App/>);await screen.findByText('Study');
  await userEvent.click(screen.getByRole('button',{name:'New folder'}));
  await userEvent.type(screen.getByLabelText('Folder name'),'Study records');
  await userEvent.click(screen.getByRole('button',{name:'Choose folder icon'}));
  await userEvent.click(screen.getByRole('button',{name:'Graduation cap'}));
  expect(screen.getByRole('button',{name:'Choose folder icon'})).toHaveFocus();
  await userEvent.click(screen.getByRole('button',{name:'Create folder'}));
  await waitFor(()=>expect(bridge.FolderWithIcon).toHaveBeenCalledWith('Study records',null,'1f393'));
  const title=await screen.findByText('Study records');expect(title.closest('.document-title')?.querySelector('img')).toHaveAttribute('src',expect.stringContaining('1f393.png'));
  await userEvent.click(screen.getByRole('button',{name:'Actions for Study records'}));
  await userEvent.click(screen.getByRole('menuitem',{name:'Edit details & move'}));
  await userEvent.click(screen.getByRole('button',{name:'Choose folder icon'}));
  expect(screen.getByRole('button',{name:'Graduation cap'})).toHaveAttribute('aria-pressed','true');
  await userEvent.click(screen.getByRole('button',{name:'Default folder'}));
  await userEvent.click(screen.getByRole('button',{name:'Save changes'}));
  await waitFor(()=>expect(bridge.Edit).toHaveBeenCalledWith('folder-rev',expect.objectContaining({folder_icon:''})));
  await waitFor(()=>expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  await userEvent.click(screen.getByText('Study records'));
  expect(screen.getByRole('navigation',{name:'Folder breadcrumb'}).querySelector('img')).toHaveAttribute('src',expect.stringContaining('1f4c1.png'));
 });
 it('preserves an edit draft across sync revision changes and stale saves, then clears it on lock',async()=>{
  const bridge=window.go!.main.App;
  render(<App/>);await screen.findByText('Passport.txt');
  await userEvent.click(screen.getByRole('button',{name:'Actions for Passport.txt'}));
  await userEvent.click(screen.getByRole('menuitem',{name:'Edit details & move'}));
  const dialog=await screen.findByRole('dialog',{name:'Document details'});
  const name=within(dialog).getByLabelText('Name');
  await userEvent.clear(name);await userEvent.type(name,'My draft.txt');
  await userEvent.type(within(dialog).getByLabelText(/^Tags/),', Personal');
  await userEvent.click(name);
  bridge.List=vi.fn(async()=>({generation:1,items:[{...item,revision:'synced-revision',name:'Remote.txt'}]}));
  await act(async()=>events['klefki:event']({generation:1,kind:'sync'}));
  expect(dialog).toBeVisible();expect(name).toHaveValue('My draft.txt');
  expect(within(dialog).getByLabelText(/^Tags/)).toHaveValue('Identity, Personal');
  expect(name).toHaveFocus();
  bridge.Edit=vi.fn(async()=>{throw new Error('STALE')});
  await userEvent.click(within(dialog).getByRole('button',{name:'Save changes'}));
  expect(await within(dialog).findByRole('alert')).toHaveTextContent('This document changed.');
  expect(name).toHaveValue('My draft.txt');expect(bridge.Edit).toHaveBeenCalledWith('revision',expect.objectContaining({name:'My draft.txt'}));
  act(()=>{state={...state,unlocked:false,generation:2};events['klefki:event']({generation:2,kind:'locked'})});
  await screen.findByText('Unlock Klefki.');
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();expect(screen.queryByDisplayValue('My draft.txt')).not.toBeInTheDocument();
 });
 it('confirms Empty Trash against the whole trash and keeps cancellation reversible',async()=>{
  const bridge=window.go!.main.App,trash={...item,trashed:true,deleteAt:Date.now()+86400000};
  bridge.List=vi.fn(async()=>({generation:1,items:[trash]}));
  bridge.PreviewDeletion=vi.fn(async revision=>({revision,count:2,targets:[{object:'object',folder:false,files:[],heads:['revision']}]}));
  bridge.DeletePermanently=vi.fn(async()=>{bridge.List=vi.fn(async()=>({generation:1,items:[]}));});
  render(<App/>);await screen.findByRole('button',{name:'View'});await userEvent.click(screen.getByRole('button',{name:'View'}));await userEvent.click(screen.getByRole('menuitem',{name:'Trash'}));
  expect(await screen.findByText(/Deletes/)).toBeVisible();
  await userEvent.type(screen.getByLabelText('Search your documents'),'passport');
  await userEvent.click(screen.getByRole('button',{name:'Empty Trash'}));
  expect(bridge.PreviewDeletion).toHaveBeenCalledWith('');
  let dialog=await screen.findByRole('dialog',{name:'Empty Trash?'});expect(dialog).toHaveTextContent('2 documents and folders');
  await userEvent.click(within(dialog).getByRole('button',{name:'Cancel'}));expect(bridge.DeletePermanently).not.toHaveBeenCalled();
  await userEvent.click(screen.getByRole('button',{name:'Empty Trash'}));dialog=await screen.findByRole('dialog',{name:'Empty Trash?'});
  await userEvent.click(within(dialog).getByRole('button',{name:'Delete permanently'}));await waitFor(()=>expect(bridge.DeletePermanently).toHaveBeenCalledTimes(1));await waitFor(()=>expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
 });
 it('keeps folder children out of All while favorites and trash include them',()=>{
  const folder={...item,object:'family',revision:'family-rev',name:'Family',type:'folder' as const};
  const child={...item,object:'child',revision:'child-rev',parent:folder.object,favorite:true};
  const trash={...child,object:'trash',revision:'trash-rev',trashed:true};
  const items=[item,folder,child,trash];
  expect(visibleItems(items,'All documents',null,'','','name')).toEqual([folder,item]);
  expect(visibleItems(items,'Folders',folder.object,'','','name')).toEqual([child]);
  expect(visibleItems(items,'Favorites',null,'','','name')).toEqual([child]);
  expect(visibleItems(items,'Trash',null,'','','name')).toEqual([trash]);
 });
 it('opens nested folders and returns to an ancestor or All in one click',async()=>{
  const folder={...item,object:'family',revision:'family-rev',name:'Family',type:'folder' as const};
  const nested={...folder,object:'nested',revision:'nested-rev',name:'Hitanshu',parent:folder.object};
  const child={...item,object:'child',revision:'child-rev',name:'Inside.txt',parent:nested.object};
  window.go!.main.App.List=vi.fn(async()=>({generation:1,items:[item,folder,nested,child]}));
  render(<App/>);await screen.findByText('Family');
  expect(screen.queryByText('Hitanshu')).not.toBeInTheDocument();
  expect(screen.queryByText('Inside.txt')).not.toBeInTheDocument();
  await userEvent.click(screen.getByText('Family'));
  expect(screen.queryByText('Passport.txt')).not.toBeInTheDocument();
  await userEvent.click(screen.getByText('Hitanshu'));
  expect(screen.getByText('Inside.txt')).toBeVisible();
  const crumbs=within(screen.getByRole('navigation',{name:'Folder breadcrumb'}));
  expect(crumbs.getByText('Hitanshu')).toHaveAttribute('aria-current','page');
  await userEvent.click(crumbs.getByRole('button',{name:'Family'}));
  expect(screen.queryByText('Inside.txt')).not.toBeInTheDocument();
  expect(screen.getByText('Hitanshu')).toBeVisible();
  await userEvent.click(crumbs.getByRole('button',{name:'All'}));
  expect(screen.getByText('Passport.txt')).toBeVisible();
  expect(screen.queryByText('Hitanshu')).not.toBeInTheDocument();
  expect(screen.queryByText('Inside.txt')).not.toBeInTheDocument();
 });
 it('opens the dedicated settings page and keeps the library search when returning',async()=>{
  render(<App/>);await screen.findByText('Passport.txt');
  await userEvent.type(screen.getByLabelText('Search your documents'),'passport');
  await userEvent.click(screen.getByRole('button',{name:'Settings'}));
  expect(screen.getByRole('heading',{name:'Settings'})).toHaveFocus();
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  expect(screen.queryByRole('textbox',{name:'Search your documents'})).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole('button',{name:'Back to library'}));
  expect(screen.getByLabelText('Search your documents')).toHaveValue('passport');
 });
 it('saves library spacing and default sorting without replacing other preferences',async()=>{
  const bridge=window.go!.main.App;
  bridge.SetPreferences=vi.fn(async p=>{state={...state,config:{...state.config,preferences:p}}});
  render(<App/>);await screen.findByText('Passport.txt');
  await userEvent.click(screen.getByRole('button',{name:'Settings'}));
  await userEvent.selectOptions(screen.getByLabelText('Document spacing'),'compact');
  await screen.findByText('Preferences saved on this device.');
  await userEvent.selectOptions(screen.getByLabelText('Default sort order'),'size');
  await waitFor(()=>expect(screen.getByLabelText('Default sort order')).toHaveValue('size'));
  expect(bridge.SetPreferences).toHaveBeenLastCalledWith({theme:'dark',idleMinutes:5,density:'compact',defaultSort:'size'});
  await userEvent.click(screen.getByRole('button',{name:'Back to library'}));
  expect(screen.getByLabelText('Sort documents')).toHaveValue('size');
  expect(document.querySelector('.library')).toHaveAttribute('data-density','compact');
 });
 it('shows failed saves and keeps recovery sync restrictions on the settings page',async()=>{
  state.recoveryOnly=true;
  const bridge=window.go!.main.App;
  bridge.SetPreferences=vi.fn(async()=>{throw new Error('OPERATION_FAILED')});bridge.Connect=vi.fn();
  render(<App/>);await screen.findByText('Passport.txt');
  await userEvent.click(screen.getByRole('button',{name:'Settings'}));
  await userEvent.selectOptions(screen.getByLabelText('Color theme'),'light');
  expect(await screen.findByRole('alert')).toHaveTextContent('could not finish');
  expect(screen.getByLabelText('Color theme')).toHaveValue('dark');
  await userEvent.click(screen.getByRole('button',{name:'Sync & devices'}));
  expect(screen.getByLabelText('Server address')).toBeDisabled();
  expect(screen.getByRole('button',{name:'Connect server'})).toBeDisabled();
  expect(screen.getByLabelText('Register this store on the server for the first time')).toBeDisabled();
  expect(screen.getByLabelText('Sync mode')).toBeDisabled();
  expect(screen.getByRole('button',{name:'Apply sync mode'})).toBeDisabled();
  expect(bridge.Connect).not.toHaveBeenCalled();
 });
 it('applies temporary relay to the connected account and forgets an old device',async()=>{
  state.config.server='https://relay.example.com';state.syncDevice='own';state.syncDevices=['own','old-device'];
  const bridge=window.go!.main.App;
  bridge.SetSyncMode=vi.fn(async mode=>{state={...state,syncMode:mode}});
  bridge.ForgetSyncDevice=vi.fn(async()=>{state={...state,syncDevices:['own']}});
  render(<App/>);await screen.findByText('Passport.txt');
  await userEvent.click(screen.getByRole('button',{name:'Settings'}));
  await userEvent.click(screen.getByRole('button',{name:'Sync & devices'}));
  expect(screen.getByLabelText('Sync mode')).toHaveValue('persistent');
  await userEvent.selectOptions(screen.getByLabelText('Sync mode'),'temporary-relay');
  expect(screen.getByText(/Offline devices keep deliveries queued/)).toBeVisible();
  await userEvent.click(screen.getByRole('button',{name:'Apply sync mode'}));
  expect(await screen.findByText('Sync mode updated for this account.')).toBeVisible();
  expect(bridge.SetSyncMode).toHaveBeenCalledWith('temporary-relay');
  expect(screen.getByLabelText('Sync mode')).toHaveValue('temporary-relay');
  await userEvent.click(screen.getByRole('button',{name:'Forget device'}));
  expect(await screen.findByText('Device removed from relay deliveries.')).toBeVisible();
  expect(bridge.ForgetSyncDevice).toHaveBeenCalledWith('old-device');
  expect(screen.getByText('This device')).toBeVisible();
  expect(screen.queryByRole('button',{name:'Forget device'})).not.toBeInTheDocument();
 });
 it('filters metadata, excludes trash, and retains conflict copies',()=>{const trash={...item,revision:'trash',trashed:true};const conflict={...item,revision:'copy',conflict:true};expect(visibleItems([item,trash,conflict],'All documents',null,'Identity','passport','name')).toHaveLength(2);expect(visibleItems([item,trash],'Trash',null,'','','name')).toEqual([trash])});
 it('searches and reports no results',async()=>{render(<App/>);await screen.findByText('Passport.txt');await userEvent.type(screen.getByLabelText('Search your documents'),'missing');expect(screen.getByText('Nothing matches yet.')).toBeInTheDocument();expect(screen.queryByText('Passport.txt')).not.toBeInTheDocument()});
 it('clears document names on lock and ignores old events',async()=>{render(<App/>);await screen.findByText('Passport.txt');await userEvent.click(screen.getByRole('button',{name:/^Lock/}));await screen.findByText('Unlock Klefki.');expect(screen.queryByText('Passport.txt')).not.toBeInTheDocument();events['klefki:event']({generation:1,kind:'file-start',operation:'old',name:'private stale name'});expect(screen.queryByText('private stale name')).not.toBeInTheDocument()});
 it('unlocks with fingerprint without submitting a password',async()=>{
 state.unlocked=false;state.fingerprint={supported:true,available:true,enabled:true,reason:''};const bridge=window.go!.main.App;bridge.FingerprintStatus=vi.fn(async()=>state.fingerprint!);bridge.UnlockFingerprint=vi.fn(async()=>{state={...state,unlocked:true,generation:2}});bridge.Unlock=vi.fn();render(<StrictMode><App/></StrictMode>);await screen.findByText('Passport.txt');expect(bridge.UnlockFingerprint).toHaveBeenCalledOnce();expect(bridge.Unlock).not.toHaveBeenCalled();expect(screen.getByText('Unlocked with fingerprint')).toBeInTheDocument();
 });
 it('keeps password fallback after a failed fingerprint',async()=>{
 state.unlocked=false;state.fingerprint={supported:true,available:true,enabled:true,reason:''};const bridge=window.go!.main.App;bridge.FingerprintStatus=vi.fn(async()=>state.fingerprint!);bridge.UnlockFingerprint=vi.fn(async()=>{throw new Error('FINGERPRINT_FAILED')});render(<App/>);expect(await screen.findByRole('alert')).toHaveTextContent('not recognized');expect(screen.getByLabelText('Password')).toBeEnabled();expect(screen.queryByText('Passport.txt')).not.toBeInTheDocument();expect(screen.queryByText('Unlocked with fingerprint')).not.toBeInTheDocument();expect(bridge.UnlockFingerprint).toHaveBeenCalledOnce();
 });
 it('shows reader retry feedback and stops animating after cancellation',async()=>{
 state.unlocked=false;state.fingerprint={supported:true,available:true,enabled:true,reason:''};const bridge=window.go!.main.App;let cancel!:()=>void;bridge.FingerprintStatus=vi.fn(async()=>state.fingerprint!);bridge.UnlockFingerprint=vi.fn(()=>new Promise<void>((_,reject)=>{cancel=()=>reject(new Error('FINGERPRINT_CANCELED'))}));bridge.CancelFingerprint=vi.fn(async()=>cancel());render(<App/>);const button=await screen.findByRole('button',{name:'Cancel fingerprint scan'});expect(button).toHaveAttribute('aria-busy','true');expect(screen.getByRole('status')).toHaveTextContent('Touch your fingerprint reader');
 expect(button.querySelector('svg')).toHaveAttribute('data-state','idle');act(()=>events['klefki:event']({generation:1,kind:'fingerprint',name:'scanning'}));expect(button.querySelector('svg')).toHaveAttribute('data-state','scanning');expect(screen.getByRole('status')).toHaveTextContent('Reading your fingerprint');act(()=>events['klefki:event']({generation:1,kind:'fingerprint',name:'touch'}));expect(button.querySelector('svg')).toHaveAttribute('data-state','idle');
 act(()=>events['klefki:event']({generation:0,kind:'fingerprint',name:'retry'}));expect(screen.getByRole('status')).toHaveTextContent('Touch your fingerprint reader');act(()=>events['klefki:event']({generation:1,kind:'fingerprint',name:'retry'}));expect(screen.getByRole('status')).toHaveTextContent('Lift your finger');expect(button.querySelector('svg')).toHaveAttribute('data-state','retry');await userEvent.click(button);await waitFor(()=>expect(button).toHaveAttribute('aria-busy','false'));expect(button).toHaveAttribute('aria-busy','false');expect(button.querySelector('svg')).toHaveAttribute('data-state','idle');expect(screen.getByLabelText('Password')).toBeEnabled();expect(screen.queryByText('Passport.txt')).not.toBeInTheDocument();
 });
 it('stops the automatic scan when typing and unlocks with the password',async()=>{
 state.unlocked=false;state.fingerprint={supported:true,available:true,enabled:true,reason:''};const bridge=window.go!.main.App;let cancel!:()=>void;bridge.FingerprintStatus=vi.fn(async()=>state.fingerprint!);bridge.UnlockFingerprint=vi.fn(()=>new Promise<void>((_,reject)=>{cancel=()=>reject(new Error('FINGERPRINT_CANCELED'))}));bridge.CancelFingerprint=vi.fn(async()=>cancel());bridge.Unlock=vi.fn(async()=>{state={...state,unlocked:true,generation:2}});render(<App/>);await screen.findByRole('button',{name:'Cancel fingerprint scan'});expect(screen.getByLabelText('Password')).toBeEnabled();await userEvent.type(screen.getByLabelText('Password'),'my password');await userEvent.click(screen.getByRole('button',{name:'Unlock Klefki'}));await screen.findByText('Passport.txt');expect(bridge.CancelFingerprint).toHaveBeenCalledOnce();expect(bridge.Unlock).toHaveBeenCalledWith('my password',false);expect(screen.getByText('Unlocked with password')).toBeInTheDocument();expect(bridge.UnlockFingerprint).toHaveBeenCalledOnce();
 });
 it('retries a timed-out scan on focus without starting duplicate scans',async()=>{
 state.unlocked=false;state.fingerprint={supported:true,available:true,enabled:true,reason:''};const bridge=window.go!.main.App;let finish!:()=>void;bridge.FingerprintStatus=vi.fn(async()=>state.fingerprint!);bridge.UnlockFingerprint=vi.fn().mockRejectedValueOnce(new Error('FINGERPRINT_CANCELED')).mockImplementation(()=>new Promise<void>(resolve=>{finish=()=>{state={...state,unlocked:true,generation:2};resolve()}}));bridge.CancelFingerprint=vi.fn(async()=>{});render(<App/>);await screen.findByRole('button',{name:'Unlock with fingerprint'});await waitFor(()=>expect(bridge.UnlockFingerprint).toHaveBeenCalledOnce());act(()=>window.dispatchEvent(new Event('focus')));await screen.findByRole('button',{name:'Cancel fingerprint scan'});act(()=>window.dispatchEvent(new Event('focus')));expect(bridge.UnlockFingerprint).toHaveBeenCalledTimes(2);await act(async()=>finish());await screen.findByText('Passport.txt');
 });
 it('waits for focus before scanning and stops when switching to recovery',async()=>{
 state.unlocked=false;state.fingerprint={supported:true,available:true,enabled:true,reason:''};const bridge=window.go!.main.App;let cancel!:()=>void;bridge.FingerprintStatus=vi.fn(async()=>state.fingerprint!);bridge.UnlockFingerprint=vi.fn(()=>new Promise<void>((_,reject)=>{cancel=()=>reject(new Error('FINGERPRINT_CANCELED'))}));bridge.CancelFingerprint=vi.fn(async()=>cancel());vi.mocked(document.hasFocus).mockReturnValue(false);render(<App/>);await screen.findByLabelText('Password');expect(bridge.UnlockFingerprint).not.toHaveBeenCalled();vi.mocked(document.hasFocus).mockReturnValue(true);act(()=>window.dispatchEvent(new Event('focus')));await screen.findByRole('button',{name:'Cancel fingerprint scan'});await userEvent.click(screen.getByRole('button',{name:'Use recovery code'}));await screen.findByLabelText('Recovery code');expect(bridge.CancelFingerprint).toHaveBeenCalledOnce();expect(screen.queryByRole('button',{name:'Unlock with fingerprint'})).not.toBeInTheDocument();expect(screen.queryByRole('alert')).not.toBeInTheDocument();
 });
 it('shows a password toast only after successful unlock and clears it on lock',async()=>{
 state.unlocked=false;const bridge=window.go!.main.App;bridge.Unlock=vi.fn().mockRejectedValueOnce(new Error('UNLOCK_FAILED')).mockImplementation(async()=>{state={...state,unlocked:true,generation:2}});render(<App/>);await userEvent.type(await screen.findByLabelText('Password'),'password');await userEvent.click(screen.getByRole('button',{name:'Unlock Klefki'}));await screen.findByRole('alert');expect(screen.queryByText('Unlocked with password')).not.toBeInTheDocument();await userEvent.click(screen.getByRole('button',{name:'Unlock Klefki'}));await screen.findByText('Passport.txt');expect(screen.getByText('Unlocked with password')).toBeInTheDocument();await userEvent.click(screen.getByRole('button',{name:'Lock'}));await screen.findByText('Unlock Klefki.');expect(screen.queryByText('Unlocked with password')).not.toBeInTheDocument();
 });
 it('verifies fingerprint setup before showing success and does not scan on removal',async()=>{
 state.fingerprint={supported:true,available:true,enabled:false,reason:''};const bridge=window.go!.main.App;let finish!:()=>void;bridge.FingerprintStatus=vi.fn(async()=>state.fingerprint!);bridge.EnableFingerprint=vi.fn(()=>new Promise<void>(resolve=>{finish=()=>{state={...state,fingerprint:{...state.fingerprint!,enabled:true}};resolve()}}));bridge.DisableFingerprint=vi.fn(async()=>{state={...state,fingerprint:{...state.fingerprint!,enabled:false}}});render(<App/>);await screen.findByText('Passport.txt');await userEvent.click(screen.getByRole('button',{name:'Settings'}));await userEvent.click(screen.getByRole('button',{name:'Privacy & lock'}));await userEvent.click(screen.getByRole('button',{name:'Enable fingerprint unlock'}));expect(screen.getByRole('status')).toHaveTextContent('Touch your fingerprint reader');expect(screen.queryByText('Fingerprint unlock enabled on this device.')).not.toBeInTheDocument();const enabling=screen.getByRole('button',{name:/Enable fingerprint unlock/});expect(enabling.querySelector('svg')).toHaveAttribute('data-state','idle');act(()=>events['klefki:event']({generation:1,kind:'fingerprint',name:'scanning'}));expect(enabling.querySelector('svg')).toHaveAttribute('data-state','scanning');act(()=>events['klefki:event']({generation:1,kind:'fingerprint',name:'touch'}));expect(enabling.querySelector('svg')).toHaveAttribute('data-state','idle');act(()=>events['klefki:event']({generation:1,kind:'fingerprint',name:'retry'}));expect(screen.getByRole('status')).toHaveTextContent('Lift your finger');await act(async()=>finish());const disable=await screen.findByRole('button',{name:'Disable fingerprint unlock'});expect(disable.querySelector('svg')).toHaveAttribute('data-state','success');expect(screen.getByRole('status')).toHaveTextContent('Fingerprint unlock enabled');await userEvent.click(disable);await screen.findByRole('button',{name:'Enable fingerprint unlock'});expect(screen.queryByRole('button',{name:'Cancel scan'})).not.toBeInTheDocument();expect(bridge.DisableFingerprint).toHaveBeenCalledOnce();expect(bridge.EnableFingerprint).toHaveBeenCalledOnce();
 });
 it('opens accessible details with prefilled fields',async()=>{render(<App/>);await screen.findByText('Passport.txt');await userEvent.click(screen.getByRole('button',{name:'Actions for Passport.txt'}));await userEvent.click(screen.getByRole('menuitem',{name:'Edit details & move'}));expect(await screen.findByRole('dialog')).toHaveAccessibleName('Document details');expect(screen.getByLabelText('Name')).toHaveValue('Passport.txt');await userEvent.keyboard('{Escape}');await waitFor(()=>expect(screen.queryByRole('dialog')).not.toBeInTheDocument())});
});

describe('document text search',()=>{
 it('shows the measured indexing time after upload and clears it on lock',async()=>{
  render(<App/>);await screen.findByText('Passport.txt');
  act(()=>events['klefki:event']({generation:1,kind:'index-start',operation:'upload'}));
  expect(screen.getByText('Indexing documents')).toBeInTheDocument();
  expect(screen.queryByText(/Search index ready/)).not.toBeInTheDocument();
  act(()=>events['klefki:event']({generation:1,kind:'index-finished',operation:'upload',durationMs:2195,name:'1 document indexed'}));
  expect(screen.getByText('Search index ready in 2.2 s')).toBeInTheDocument();
  act(()=>events['klefki:event']({generation:1,kind:'finished',operation:'upload'}));
  expect(screen.queryByRole('complementary',{name:'File transfers'})).not.toBeInTheDocument();
  expect(screen.queryByText('Saved successfully')).not.toBeInTheDocument();
  expect(screen.queryByText('1 document indexed')).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole('button',{name:/^Lock/}));
  await screen.findByText('Unlock Klefki.');
  expect(screen.queryByText(/Search index ready/)).not.toBeInTheDocument();
  act(()=>events['klefki:event']({generation:1,kind:'index-finished',operation:'old',durationMs:500}));
  expect(screen.queryByText(/Search index ready/)).not.toBeInTheDocument();
 });
 it('retains partial upload failures alongside the indexing toast',async()=>{
  render(<App/>);await screen.findByText('Passport.txt');
  act(()=>{
   events['klefki:event']({generation:1,kind:'file-finished',operation:'upload',name:'Missing.txt',error:'OPERATION_FAILED'});
   events['klefki:event']({generation:1,kind:'index-finished',operation:'upload',durationMs:100});
   events['klefki:event']({generation:1,kind:'finished',operation:'upload',error:'IMPORT_PARTIAL'});
  });
  expect(screen.getByText('Search index ready in 100 ms')).toBeInTheDocument();
  expect(screen.getByRole('complementary',{name:'File transfers'})).toHaveTextContent('Some files could not be added.');
  expect(screen.getByText('Missing.txt')).toBeInTheDocument();
  await userEvent.click(screen.getByRole('button',{name:'Dismiss transfer'}));
  expect(screen.queryByRole('complementary',{name:'File transfers'})).not.toBeInTheDocument();
 });
 it('reports indexing failures without a success toast',async()=>{
  render(<App/>);await screen.findByText('Passport.txt');
  act(()=>events['klefki:event']({generation:1,kind:'index-start',operation:'upload'}));
  act(()=>events['klefki:event']({generation:1,kind:'index-finished',operation:'upload',durationMs:100,error:'SEARCH_EXTRACTION_FAILED'}));
  expect(screen.getByRole('alert')).toHaveTextContent('Your documents are saved, but indexing failed. Text extraction failed.');
  expect(screen.queryByText(/Search index ready/)).not.toBeInTheDocument();
 });
 it('shows milliseconds for a fast index and dismisses the toast',async()=>{
  render(<App/>);await screen.findByText('Passport.txt');
  vi.useFakeTimers();
  try{
   act(()=>events['klefki:event']({generation:1,kind:'index-finished',operation:'upload',durationMs:34.4}));
   expect(screen.getByText('Search index ready in 34 ms')).toBeInTheDocument();
   act(()=>vi.advanceTimersByTime(4500));
   expect(screen.queryByText(/Search index ready/)).not.toBeInTheDocument();
  }finally{vi.useRealTimers()}
 });
 it('shows content matches and starts a local rebuild',async()=>{
  const bridge=window.go!.main.App;
  bridge.Search=vi.fn(async()=>({generation:1,hits:[{object:item.object,revision:item.revision,name:item.name,score:1}]}));
  bridge.RebuildIndex=vi.fn(async()=> 'index-operation');
  render(<App/>);await screen.findByText('Passport.txt');
  await userEvent.type(screen.getByLabelText('Search your documents'),'nebula');
  await waitFor(()=>expect(bridge.Search).toHaveBeenCalledWith('nebula'));
  expect(await screen.findByText('Passport.txt')).toBeInTheDocument();
  await userEvent.click(screen.getByRole('button',{name:'Rebuild search index'}));
  expect(bridge.RebuildIndex).toHaveBeenCalledOnce();
 });
 it('shows a stale-index explanation and ignores results arriving after lock',async()=>{
  const bridge=window.go!.main.App;
  bridge.Search=vi.fn(async()=>{throw Error('SEARCH_REBUILD_REQUIRED')});
  render(<App/>);await screen.findByText('Passport.txt');
  await userEvent.type(screen.getByLabelText('Search your documents'),'nebula');
  await screen.findByText('Rebuild the search index to search document text. Names and tags still work.');
  let resolve!:(v:any)=>void;
  bridge.Search=vi.fn(()=>new Promise<Awaited<ReturnType<typeof bridge.Search>>>(r=>{resolve=r}));
  await userEvent.type(screen.getByLabelText('Search your documents'),'x');
  await waitFor(()=>expect(bridge.Search).toHaveBeenCalled());
  await userEvent.click(screen.getByRole('button',{name:/^Lock/}));
  await screen.findByText('Unlock Klefki.');
  await act(async()=>resolve({generation:1,hits:[{object:item.object,revision:item.revision,name:item.name,score:1}]}));
  expect(screen.queryByText('Passport.txt')).not.toBeInTheDocument();
 });
});
