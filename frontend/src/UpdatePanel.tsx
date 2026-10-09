import { useState } from 'react'
import { Browser } from '@wailsio/runtime'
import { UpdateService } from '../bindings/crab.gitsync'
import type { UpdateState } from '../bindings/crab.gitsync/models'

const labels: Record<string,string> = { idle:'尚未检查', checking:'正在检查发布版本', 'up-to-date':'没有可用的新版本', available:'发现新版本', downloading:'正在下载更新', verifying:'正在校验文件', installing:'正在准备更新', ready:'更新已准备好', restarting:'正在重启应用', cancelled:'更新已取消', error:'更新检查或下载失败' }
const mb = (bytes: number) => `${(bytes / 1048576).toFixed(1)} MB`

export default function UpdatePanel({ state, gitBusy, connected, onChange }: { state: UpdateState, gitBusy:boolean, connected:boolean, onChange:(state:UpdateState)=>void }) {
  const [error,setError]=useState('')
  const [pending,setPending]=useState(false)
  const act=async(action:()=>Promise<unknown>)=>{
    setPending(true);setError('')
    try { await action();onChange(await UpdateService.GetState()) } catch(e){setError(String(e))} finally {setPending(false)}
  }
  const percent=state.total>0?Math.min(100,Math.round(state.written/state.total*100)):0
  return <section className="update-panel" aria-labelledby="update-heading">
    <div className="update-top"><div><h2 id="update-heading">应用更新 <span className="tag blue">v{state.version || '…'}</span></h2><p>从 GitHub Releases 获取新版 · 启动时与每 6 小时自动检查</p></div><button disabled={!connected || state.busy || pending || state.phase==='ready'} onClick={()=>void act(()=>UpdateService.StartCheck())}>检查更新</button></div>
    <div className="update-status" role="status"><strong>{labels[state.phase] || state.phase}{state.latestVersion && ` · v${state.latestVersion}`}</strong><span>{state.platform}{state.authSource && ` · ${state.authSource}`}</span></div>
    {['downloading','verifying','installing','ready'].includes(state.phase) && <><div className="progress-track" role="progressbar" aria-label="更新下载进度" aria-valuemin={0} aria-valuemax={100} aria-valuenow={percent}><div style={{width:`${percent}%`}}/></div><p className="update-size">{mb(state.written)} / {mb(state.total)}{state.phase==='ready' && ' · SHA-256 校验通过'}</p></>}
    {(error || state.error) && <p className="update-error" role="alert">{error || state.error}</p>}
    {state.notes && <details className="release-notes"><summary>查看版本说明</summary><p>{state.notes}</p></details>}
    <div className="update-actions">
      {state.latestVersion && !state.busy && state.phase!=='ready' && <button className="primary" disabled={!connected || pending} onClick={()=>void act(()=>UpdateService.StartDownload())}>下载更新</button>}
      {state.phase==='ready' && <button className="primary" disabled={gitBusy || pending} onClick={()=>void act(()=>UpdateService.Restart())}>重启应用更新</button>}
      {state.busy && state.phase!=='restarting' && <button disabled={pending} onClick={()=>void act(()=>UpdateService.Cancel())}>取消更新任务</button>}
      <button className="text-button" onClick={()=>void Browser.OpenURL('https://github.com/CrabGo/Crab.GitSync/releases')}>查看发布页面</button>
      <span>{gitBusy && state.phase==='ready' ? 'Git 任务完成后可重启更新' : state.phase==='ready' ? '重启后自动替换程序，保留扫描路径' : state.checkedAt ? `上次检查 ${new Date(state.checkedAt).toLocaleString('zh-CN')}` : '公开发布源，无需 GitHub 登录或令牌'}</span>
    </div>
  </section>
}
