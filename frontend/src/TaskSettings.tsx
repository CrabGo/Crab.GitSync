import { useEffect, useState } from 'react'
import { GitService } from '../bindings/crab.gitsync'

export default function TaskSettings({connected,busy}:{connected:boolean,busy:boolean}) {
  const [limit,setLimit]=useState(3)
  const [historyTasks,setHistoryTasks]=useState(100)
  const [historyDays,setHistoryDays]=useState(30)
  const [pending,setPending]=useState(false)
  const [message,setMessage]=useState('')
  useEffect(()=>{if(!connected)return;let active=true;void GitService.GetTaskConfig().then(c=>{if(active){setLimit(c.concurrency);setHistoryTasks(c.historyTasks);setHistoryDays(c.historyDays)}}).catch(e=>{if(active)setMessage(String(e))});return()=>{active=false}},[connected])
  const save=async()=>{setPending(true);setMessage('');try{await GitService.SaveTaskConfig({concurrency:limit,historyTasks,historyDays});setMessage('已保存；历史按新策略清理，下次获取使用此并发数')}catch(e){setMessage(String(e))}finally{setPending(false)}}
  return <section className="scan-panel"><details><summary>获取任务设置</summary><p className="scan-hint">默认同时获取 3 个仓库，可设置 1–5 个。共享 Git 元数据的 worktree 仍互斥；取消后等待在途任务退出。</p><p className="scan-hint">历史默认保留最近 100 个任务或 30 天，任一限制达到即清理。保存保留策略后立即清理已完成记录，不影响进行中的任务。</p><div className="scan-list-controls"><label>并发仓库数 <select aria-label="并发仓库数" disabled={busy||pending||!connected} value={limit} onChange={e=>setLimit(Number(e.target.value))}>{[1,2,3,4,5].map(n=><option key={n} value={n}>{n}</option>)}</select></label><label>保留任务数 <input aria-label="历史保留任务数" type="number" min="1" max="1000" disabled={busy||pending||!connected} value={historyTasks} onChange={e=>setHistoryTasks(Number(e.target.value))}/></label><label>保留天数 <input aria-label="历史保留天数" type="number" min="1" max="365" disabled={busy||pending||!connected} value={historyDays} onChange={e=>setHistoryDays(Number(e.target.value))}/></label><button disabled={busy||pending||!connected||historyTasks<1||historyTasks>1000||historyDays<1||historyDays>365} onClick={()=>void save()}>保存任务设置</button>{message&&<small role="status">{message}</small>}</div></details></section>
}
