import { useEffect, useState } from 'react'
import { GitService } from '../bindings/crab.gitsync'
import type { List } from '../bindings/crab.gitsync/internal/scansettings/models'

const lines=(text:string)=>text.split(/\r?\n/).map(value=>value.trim()).filter(Boolean)
export default function ScanLists({connected,busy,defaultPath,onScan}:{connected:boolean,busy:boolean,defaultPath:string,onScan:(id:string)=>Promise<unknown>}) {
  const [lists,setLists]=useState<List[]>([])
  const [id,setID]=useState('')
  const [name,setName]=useState('')
  const [roots,setRoots]=useState('')
  const [excludes,setExcludes]=useState('')
  const [pending,setPending]=useState(false)
  const [dirty,setDirty]=useState(false)
  const [error,setError]=useState('')
  const [removing,setRemoving]=useState(false)
  const [scheduled,setScheduled]=useState(false)
  const [intervalMinutes,setIntervalMinutes]=useState(30)
  const [scheduleStatus,setScheduleStatus]=useState<Record<string,{nextRun:string,lastRun:string,status:string}>>({})
  const select=(list?:List)=>{setID(list?.id || '');setName(list?.name || '');setRoots(list?.roots?.join('\n') || defaultPath);setExcludes(list?.excludes?.join('\n') || '');setScheduled(list?.scheduled || false);setIntervalMinutes(list?.intervalMinutes || 30);setDirty(!list);setRemoving(false);setError('')}
  useEffect(()=>{if(!connected)return;let active=true;const poll=()=>{void GitService.GetSchedules().then(values=>{if(active)setScheduleStatus(Object.fromEntries((values||[]).map(value=>[value.listID,value])))}).catch(()=>{})};poll();const timer=setInterval(poll,5000);return()=>{active=false;clearInterval(timer)}},[connected])
  useEffect(()=>{
    if(!connected)return
    let active=true
    void GitService.GetScanLists().then(values=>{
      if(!active)return
      const available=values || [];setLists(available)
      let last='';try{last=localStorage.getItem('crab.scanList') || ''}catch{/* Selection can still be used without storage. */}
      select(available.find(value=>value.id===last) || available[0])
    }).catch(e=>{if(active)setError(String(e))})
    return()=>{active=false}
  },[connected])
  const save=async()=>{
    setPending(true);setError('')
    try{const saved=await GitService.SaveScanList({id,name,roots:lines(roots),excludes:lines(excludes),scheduled,intervalMinutes});setLists(await GitService.GetScanLists() || []);select(saved);try{localStorage.setItem('crab.scanList',saved.id)}catch{/* The list itself is saved by the backend. */}}catch(e){setError(String(e))}finally{setPending(false)}
  }
  const saveSchedule=async(enabled:boolean,applyInterval=true)=>{
    const base=lists.find(value=>value.id===id);if(!base)return
    setPending(true);setError('')
    try{const saved=await GitService.SaveScanList({...base,scheduled:enabled,intervalMinutes:!applyInterval&&!enabled?base.intervalMinutes:intervalMinutes});setScheduled(saved.scheduled);setIntervalMinutes(saved.intervalMinutes);setLists(old=>old.map(value=>value.id===id?saved:value));const states=await GitService.GetSchedules();setScheduleStatus(Object.fromEntries((states||[]).map(value=>[value.listID,value])))}catch(e){setError(String(e))}finally{setPending(false)}
  }
  const remove=async()=>{
    setPending(true);setError('')
    try{await GitService.RemoveScanList(id);const available=await GitService.GetScanLists() || [];setLists(available);select(available[0])}catch(e){setError(String(e))}finally{setPending(false)}
  }
  const choose=async()=>{
    setPending(true);setError('')
    try{const value=await GitService.ChooseDirectory();if(value){setRoots(old=>[...lines(old),value].join('\n'));setDirty(true)}}catch(e){setError(String(e))}finally{setPending(false)}
  }
  const scan=async()=>{
    setPending(true);setError('')
    try{localStorage.setItem('crab.scanList',id)}catch{/* Scanning does not require local storage. */}
    try{await onScan(id)}catch(e){setError(String(e))}finally{setPending(false)}
  }
  return <section className="scan-panel scan-lists"><details><summary>多路径扫描列表 · {lists.length} 个已保存</summary>
    <p className="scan-hint">保存命名列表，一次扫描多个目录。重叠仓库自动去重，无法读取的路径记录警告后继续。</p>
    <fieldset disabled={busy||pending||!connected}>
      <div className="scan-list-controls"><label>已保存列表<select aria-label="已保存扫描列表" value={id} onChange={e=>select(lists.find(value=>value.id===e.target.value))}><option value="">新建列表</option>{lists.map(list=><option key={list.id} value={list.id}>{list.name}</option>)}</select></label><button onClick={()=>select()}>新建列表</button></div>
      <label className="scan-list-field">列表名称<input aria-label="扫描列表名称" maxLength={80} value={name} placeholder="例如：工作仓库" onChange={e=>{setName(e.target.value);setDirty(true)}}/></label>
      <div className="scan-list-fields"><label>扫描根目录（每行一个）<textarea aria-label="扫描列表根目录" rows={4} value={roots} onChange={e=>{setRoots(e.target.value);setDirty(true)}}/></label><label>排除目录（每行一个，可选）<textarea aria-label="扫描列表排除目录" rows={4} value={excludes} placeholder="build&#10;archive" onChange={e=>{setExcludes(e.target.value);setDirty(true)}}/></label></div>
      <p className="scan-hint">排除目录填写相对于每个根目录的路径或绝对路径，不支持通配符。默认继续跳过 .git、node_modules、.venv，不跟随子目录符号链接。</p>
      <div className="scan-list-controls"><button onClick={()=>void choose()}>添加目录</button><button disabled={!name.trim()||!lines(roots).length} onClick={()=>void save()}>保存扫描列表</button><button disabled={!id} onClick={()=>setRemoving(true)}>删除列表</button><button className="primary" disabled={!id||dirty} onClick={()=>void scan()}>扫描此列表</button>{dirty&&<small>保存后可扫描</small>}</div>
      {removing&&<div className="scan-list-controls"><span>确认删除「{name}」？仓库和路径历史将保留。</span><button onClick={()=>setRemoving(false)}>取消删除</button><button onClick={()=>void remove()}>确认删除列表</button></div>}
    </fieldset>
    <fieldset disabled={pending||!connected||!id} className="schedule-controls">
      <label><input type="checkbox" aria-label="启用此列表定时获取" checked={scheduled} onChange={e=>void saveSchedule(e.target.checked,false)}/>定时获取此列表（默认关闭）</label>
      <label>间隔（分钟）<input type="number" aria-label="定时获取间隔" min={1} max={1440} value={intervalMinutes} onChange={e=>setIntervalMinutes(Number(e.target.value))}/></label>
      <button disabled={intervalMinutes<1||intervalMinutes>1440||!Number.isInteger(intervalMinutes)} onClick={()=>void saveSchedule(scheduled)}>保存定时间隔</button>
      <p className="scan-hint">只自动 fetch，不合并或撤销。忙碌时跳过；休眠恢复后不补跑积压任务。关闭立即停止后续调度，已开始任务可用取消按钮停止。仅失败或发现远端新提交时通知。</p>
      {id&&<p role="status">{({disabled:'已关闭',waiting:'等待执行',running:'正在定时获取','busy-skip':'上次到期时忙碌，已跳过',done:'上次已完成',error:'上次有错误，请查看日志',cancelled:'上次已取消'} as Record<string,string>)[scheduleStatus[id]?.status]||'等待加载'}{scheduleStatus[id]?.nextRun&&` · 下次 ${new Date(scheduleStatus[id].nextRun).toLocaleString('zh-CN')}`}</p>}
    </fieldset>
    {error&&<p className="update-error" role="alert">{error}</p>}
  </details></section>
}
