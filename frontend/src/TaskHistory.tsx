import { useEffect, useState } from 'react'
import { GitService } from '../bindings/crab.gitsync'
import type { Record as HistoricalRecord, Summary } from '../bindings/crab.gitsync/internal/taskhistory/models'
import TaskResults from './TaskResults'

const kinds:Record<string,string>={scan:'扫描',fetch:'获取远端',merge:'合并',discard:'撤销修改','pull-merge':'拉取并合并','abort-merge':'中止合并'}
export default function TaskHistory({connected,taskID,finishedAt,canRetry,onRetry}:{connected:boolean,taskID:string,finishedAt:string,canRetry:boolean,onRetry:(id:string)=>void}) {
  const [policy,setPolicy]=useState({historyTasks:100,historyDays:30})
  const [tasks,setTasks]=useState<Summary[]>([])
  const [id,setID]=useState('')
  const [record,setRecord]=useState<HistoricalRecord|null>(null)
  const [warning,setWarning]=useState('')
  const [error,setError]=useState('')
  const [loading,setLoading]=useState(false)
  useEffect(()=>{if(!connected)return;let active=true;void Promise.all([GitService.ListTaskHistory(),GitService.GetHistoryWarning(),GitService.GetTaskConfig().catch(()=>null)]).then(([values,message,config])=>{if(active){setTasks(values||[]);setWarning(message);if(config)setPolicy({historyTasks:config.historyTasks,historyDays:config.historyDays});setID(old=>values?.some(t=>t.id===old)?old:values?.[0]?.id||'')}}).catch(e=>{if(active)setError(String(e))});return()=>{active=false}},[connected,taskID,finishedAt])
  useEffect(()=>{let active=true;setRecord(null);setError('');setLoading(false);if(!id)return;setLoading(true);void GitService.GetTaskHistory(id).then(value=>{if(active)setRecord(value)}).catch(e=>{if(active)setError(String(e))}).finally(()=>{if(active)setLoading(false)});return()=>{active=false}},[id])
  const exportLogs=()=>{if(!record)return;const text=(record.logs||[]).map(l=>`${l.time} [${l.level}] ${l.message}`).join('\n');const url=URL.createObjectURL(new Blob(['\ufeff'+text],{type:'text/plain;charset=utf-8'}));const a=document.createElement('a');a.href=url;a.download='crab-gitsync-history.log';a.click();setTimeout(()=>URL.revokeObjectURL(url),1000)}
  return <section className="update-panel task-results"><h2>已完成任务历史</h2><p>当前保留最多 {policy.historyTasks} 个任务或 {policy.historyDays} 天，任一限制达到即清理；可在工作台任务设置中调整。进行中的任务不归档、不清理。每个任务保存最多 500 条脱敏日志。</p>{warning&&<p className="update-error" role="alert">{warning}</p>}{error&&<p className="update-error" role="alert">{error}</p>}<label>选择任务 <select aria-label="已完成任务" disabled={!connected||!tasks.length} value={id} onChange={e=>setID(e.target.value)}><option value="">选择已完成任务</option>{tasks.map(t=><option key={t.id} value={t.id}>{new Date(t.finishedAt).toLocaleString()} · {kinds[t.kind]||t.kind} · 成功 {t.succeeded} / 错误 {t.failed}</option>)}</select></label>{!tasks.length&&<p>暂无已完成任务。完成扫描或获取后，记录会在此保存。</p>}{loading&&<p>正在读取历史…</p>}{record&&<><p>{kinds[record.kind]||record.kind} · {record.phase==='cancelled'?'已取消':record.phase==='error'?'失败':'已完成'} · {record.completed}/{record.total} · 跳过 {record.skipped}</p><p>{record.root}</p><TaskResults label="历史任务结果" results={record.results||[]} taskID={record.id} sourceTaskID={record.sourceTaskID} canRetry={canRetry&&record.kind==='fetch'} onRetry={()=>onRetry(record.id)}/><details><summary>历史任务日志 · {record.logs?.length||0} 条</summary><button onClick={exportLogs}>导出此任务日志</button><pre>{(record.logs||[]).map(l=>`${l.time} [${l.level}] ${l.message}`).join('\n')}</pre></details></>}</section>
}
