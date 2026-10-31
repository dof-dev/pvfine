import { beforeEach, describe, expect, it, vi } from 'vitest';
import { createPinia, setActivePinia } from 'pinia';
import { nextTick } from 'vue';
import type { ItemCreateRequest } from '../bindings/pvfine/services/models';
const api=vi.hoisted(()=>({GetFileBasic:vi.fn(),GetFile:vi.fn(),GetAnnotations:vi.fn(),CreateItem:vi.fn(),refreshInfo:vi.fn(),events:new Map<string,Array<()=>void>>()}));
vi.mock('@wailsio/runtime',()=>({Events:{On:(name:string,fn:()=>void)=>api.events.set(name,[...(api.events.get(name)??[]),fn])}}));
vi.mock('../bindings/pvfine/services',()=>({EditorService:api,ArchiveService:api,FileGUIService:api}));
vi.mock('../src/stores/archive',()=>({useArchiveStore:()=>({refreshInfo:api.refreshInfo})}));
vi.mock('../src/stores/script',()=>({useScriptStore:()=>({})}));
vi.mock('../src/stores/explorer',()=>({useExplorerStore:()=>({getFilePath:()=>'',refreshTreeTags:vi.fn()})}));
import { useEditorStore } from '../src/stores/editor';
import { useFileGUIStore } from '../src/stores/fileGUI';
const meta=(index:number)=>({index,path:index===1?'stackable/stackable.lst':'stackable/source.stk',text:'original',editable:true,dataType:1,size:8,tags:[],annotations:[],modified:false});
const request:ItemCreateRequest={path:'stackable/new.stk',id:'999',name:'新物品',text:'[name]\n`copy`',revision:1,drafts:[]};
beforeEach(()=>{setActivePinia(createPinia());vi.resetAllMocks();api.events.clear();api.GetFileBasic.mockImplementation(async i=>meta(i));api.GetAnnotations.mockResolvedValue([]);api.GetFile.mockResolvedValue(null);});
describe('物品创建工作区',()=>{
 it('提交包含所有未保存草稿，仅推进被消费列表的基线',async()=>{
  const editor=useEditorStore();await editor.openGUIFile(1);await editor.openGUIFile(2);
  editor.updateContent(1,'list draft');editor.updateContent(2,'source draft');
  api.CreateItem.mockResolvedValue({fileIndex:3,path:request.path,id:'999',files:[{fileIndex:1,path:'stackable/stackable.lst',beforeText:'list draft',text:'registered list'}]});
  await editor.createItem(request);
  expect(api.CreateItem.mock.calls[0]![0].drafts).toEqual([{fileIndex:1,path:'stackable/stackable.lst',text:'list draft'},{fileIndex:2,path:'stackable/source.stk',text:'source draft'}]);
  expect(editor.getFileDraft(1)).toMatchObject({text:'registered list',original:'registered list',modified:true});
  expect(editor.getFileDraft(2)).toMatchObject({text:'source draft',original:'original'});
  expect(editor.dirtyCount).toBe(1);expect(editor.saving).toBe(false);expect(editor.guiApplying).toBe(false);
 });
 it('创建期间锁定编辑，失败后保留草稿和状态',async()=>{
  const editor=useEditorStore();await editor.openGUIFile(1);editor.updateContent(1,'draft');
  let reject!:(e:Error)=>void;api.CreateItem.mockImplementation(()=>new Promise((_,r)=>{reject=r;}));
  const work=editor.createItem(request);await nextTick();
  expect(editor.guiApplying).toBe(true);editor.updateContent(1,'unexpected');expect(editor.getFileDraft(1)?.text).toBe('draft');
  reject(new Error('ID conflict'));await expect(work).rejects.toThrow('ID conflict');
  expect(editor.getFileDraft(1)).toMatchObject({text:'draft',original:'original'});expect(editor.guiApplying).toBe(false);
 });
 it('拒绝跨归档的旧返回值',async()=>{
  const editor=useEditorStore(),gui=useFileGUIStore();await editor.openGUIFile(1);editor.updateContent(1,'draft');
  let resolve!:(v:unknown)=>void;api.CreateItem.mockImplementation(()=>new Promise(r=>{resolve=r;}));
  const work=editor.createItem(request);await nextTick();gui.epoch++;
  resolve({fileIndex:3,path:request.path,id:'999',files:[{fileIndex:1,path:'stackable/stackable.lst',beforeText:'draft',text:'old response'}]});
  await expect(work).rejects.toThrow('归档已切换');expect(editor.getFileDraft(1)?.text).toBe('draft');expect(editor.saving).toBe(false);
 });
 it('刷新失败仍返回已创建结果并保留提示，避免重复创建',async()=>{
  const editor=useEditorStore();api.CreateItem.mockResolvedValue({fileIndex:3,path:request.path,id:'999',files:[]});api.refreshInfo.mockRejectedValue(new Error('refresh'));
  expect((await editor.createItem(request)).fileIndex).toBe(3);expect(editor.guiRefreshWarning).toContain('已创建');
 });
});
