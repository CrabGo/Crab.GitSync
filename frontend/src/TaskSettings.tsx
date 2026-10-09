import { useEffect, useState } from 'react'
import { GitService } from '../bindings/crab.gitsync'

export default function TaskSettings({connected,busy}:{connected:boolean,busy:boolean}) {
  const [limit,setLimit]=useState(3)
  const [pending,setPending]=useState(false)
  const [message,setMessage]=useState('')
  useEffect(()=>{if(!connected)return;let active=true;void GitService.GetTaskConfig().then(c=>{if(active)setLimit(c.concurrency)}).catch(e=>{if(active)setMessage(String(e))});return()=>{active=false}},[connected])
  const save=async()=>{setPending(true);setMessage('');try{await GitService.SaveTaskConfig({concurrency:limit});setMessage('已保存，下次获取任务使用此并发数')}catch(e){setMessage(String(e))}finally{setPending(false)}}
  return <section className="scan-panel"><details><summary>获取任务设置</summary><p className="scan-hint">默认同时获取 3 个仓库，可设置 1–5 个。共享 Git 元数据的 worktree 仍互斥；取消后等待在途任务退出。</p><div className="scan-list-controls"><label>并发仓库数 <select aria-label="并发仓库数" disabled={busy||pending||!connected} value={limit} onChange={e=>setLimit(Number(e.target.value))}>{[1,2,3,4,5].map(n=><option key={n} value={n}>{n}</option>)}</select></label><button disabled={busy||pending||!connected} onClick={()=>void save()}>保存任务设置</button>{message&&<small role="status">{message}</small>}</div></details></section>
}
