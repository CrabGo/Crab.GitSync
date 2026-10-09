import type { Result } from '../bindings/crab.gitsync/internal/taskresult/models'
import { useEffect, useState } from 'react'
import { GitService } from '../bindings/crab.gitsync'
const categories:Record<string,string>={unknown:'未知错误',cancelled:'已取消',timeout:'超时',tls:'证书/TLS',authentication:'认证',dns:'DNS',proxy:'代理连接',remote_not_found:'远端地址/权限',connection:'网络连接',refresh:'状态刷新',git_state:'仓库状态'}
const stages:Record<string,string>={fetch:'获取远端',refresh:'刷新状态',inspect:'读取仓库',action:'仓库操作',queued:'等待执行','retry-wait':'等待网络重试'}
const status:Record<string,string>={success:'成功',error:'失败',skipped:'跳过',cancelled:'取消',queued:'等待执行',running:'正在执行'}
export default function TaskResults({results,taskID,sourceTaskID,canRetry,onRetry}:{results:Result[],taskID:string,sourceTaskID:string,canRetry:boolean,onRetry:()=>void}) {
  const [source,setSource] = useState<Result[]>([])
  const [sourceError,setSourceError] = useState('')
  useEffect(()=>{let active=true;setSource([]);setSourceError('');if(sourceTaskID)void GitService.GetFetchResults(sourceTaskID).then(value=>{if(active)setSource(value || [])}).catch(e=>{if(active)setSourceError(String(e))});return()=>{active=false}},[sourceTaskID])
  if(!results.length)return null
  return <section className="update-panel task-results">
    {results.some(r=>r.status==='error') && <div className="retry-controls"><button disabled={!canRetry} onClick={onRetry}>仅重试失败仓库</button><small>成功、跳过及取消项不重试；状态刷新失败仅重新读取本地信息。</small></div>}
    <details><summary>当前任务结果 · {results.length} 个仓库</summary><small>{taskID}</small>{sourceTaskID && <details><summary>查看来源任务结果 · {sourceTaskID}</summary><p>原任务结果保留在当前会话最近 10 次获取任务中。</p>{sourceError && <p role="alert">{sourceError}</p>}{source.map(r=><div className="task-result" key={r.path}><strong>{r.path}</strong><span>{status[r.status]||r.status} · {stages[r.stage]||r.stage}</span>{r.failure && <p>{r.failure.message}</p>}</div>)}</details>}{results.map(r=><div className="task-result" key={r.path}><strong>{r.path}</strong><span>{status[r.status]||r.status} · {stages[r.stage]||r.stage} · {r.attempts} 次尝试 · {(r.durationMS/1000).toFixed(1)} 秒</span>{r.networkSucceeded && r.failure && <p>远端获取已成功，本地状态尚未刷新。</p>}{r.failure && <><p>{categories[r.failure.category]||r.failure.category}：{r.failure.message}{r.failure.retryable?' · 可重试':' · 需检查后处理'}</p><details><summary>错误详情</summary><pre>{r.failure.detail}</pre></details></>}</div>)}</details></section>
}
