import { useEffect, useState } from 'react'
import { NetworkService } from '../bindings/crab.gitsync'
import type { State as DiagnosisState } from '../bindings/crab.gitsync/internal/diagnostics/models'

export default function NetworkPanel({ connected }: { connected: boolean }) {
  const [config,setConfig]=useState({enabled:true,protocol:'http',host:'127.0.0.1',port:33210})
  const [loaded,setLoaded]=useState(false)
  const [pending,setPending]=useState(false)
  const [error,setError]=useState('')
  const [saved,setSaved]=useState(false)
  const [remote,setRemote]=useState('https://github.com/CrabGo/Crab.GitSync.git')
  const [diagnosis,setDiagnosis]=useState<DiagnosisState>({busy:false,status:'idle',strategy:'',remote:'',startedAt:'',finishedAt:'',steps:[]})
  const [diagnosing,setDiagnosing]=useState(false)
  useEffect(()=>{
    if(!connected)return
    let disposed=false
    let timer:ReturnType<typeof setTimeout>
    const poll=async()=>{try{const state=await NetworkService.GetDiagnosis();if(!disposed)setDiagnosis(state)}catch(e){if(!disposed)setError(String(e))}if(!disposed)timer=setTimeout(poll,500)}
    void poll();return()=>{disposed=true;clearTimeout(timer)}
  },[connected])
  const diagnose=async()=>{
    setDiagnosing(true);setError('')
    try{await NetworkService.StartDiagnosis(remote);setDiagnosis(await NetworkService.GetDiagnosis())}catch(e){setError(String(e))}finally{setDiagnosing(false)}
  }
  useEffect(()=>{
    if(!connected)return
    let disposed=false
    void NetworkService.GetConfig().then(value=>{if(!disposed){setConfig(value);setLoaded(true)}}).catch(e=>{if(!disposed){setError(String(e));setLoaded(true)}})
    return ()=>{disposed=true}
  },[connected])
  const save=async()=>{
    setPending(true);setError('');setSaved(false)
    try {await NetworkService.SaveConfig(config);setConfig(await NetworkService.GetConfig());setSaved(true)}catch(e){setError(String(e))}finally{setPending(false)}
  }
  return <section className="update-panel network-panel" aria-labelledby="network-heading">
    <div className="update-top"><div><h2 id="network-heading">代理配置</h2><p>用于 HTTPS 仓库获取远端更新，以及应用版本检查和下载</p></div></div>
    <fieldset disabled={!connected || !loaded || pending} onChange={()=>setSaved(false)}>
      <label className="proxy-toggle"><input type="checkbox" checked={config.enabled} onChange={e=>setConfig({...config,enabled:e.target.checked})}/>启用应用代理</label>
      <div className="proxy-fields">
        <label>代理类型<select value={config.protocol} onChange={e=>setConfig({...config,protocol:e.target.value})}><option value="http">HTTP</option><option value="socks5h">SOCKS5（远端 DNS）</option></select></label>
        <label>代理主机<input value={config.host} placeholder="127.0.0.1" onChange={e=>setConfig({...config,host:e.target.value})}/></label>
        <label>代理端口<input type="number" min="1" max="65535" value={config.port} onChange={e=>setConfig({...config,port:Number(e.target.value)})}/></label>
      </div>
      <p className="proxy-hint">默认 HTTP · 127.0.0.1:33210。代理程序需要已启动。关闭后沿用系统、环境变量及 Git 原有网络配置。SSH 仓库沿用本机 SSH 配置。</p>
      <button className="primary" onClick={()=>void save()}>{pending?'正在保存…':'保存配置'}</button>
    </fieldset>
    {saved && <p className="proxy-saved" role="status">配置已保存，新网络请求生效；运行中的 Git 任务使用启动时配置。</p>}
    {error && <p className="update-error" role="alert">{error}</p>}
    <div className="diagnosis-panel"><h3>连接诊断</h3><p className="proxy-hint">使用已保存配置，分步测试代理端口、GitHub HTTPS 和指定 Git 远端。诊断只读取信息，可随时取消。</p>
      <label className="diagnosis-remote">Git 仓库地址<input value={remote} disabled={diagnosis.busy||diagnosing} onChange={e=>setRemote(e.target.value)} placeholder="https://github.com/owner/repo.git"/></label>
      <div className="update-actions"><button disabled={!connected||diagnosis.busy||diagnosing} onClick={()=>void diagnose()}>测试连接</button>{diagnosis.busy&&<button onClick={()=>void NetworkService.CancelDiagnosis().catch(e=>setError(String(e)))}>取消诊断</button>}<span>{diagnosis.strategy}</span></div>
      <div role="status">{diagnosis.steps?.map(step=><div className="task-result" key={step.name}><strong>{step.name} · {{queued:'等待测试',running:'测试中',success:'连接成功',error:'失败',skipped:'跳过',cancelled:'已取消'}[step.status]||step.status}</strong><span>{(step.durationMS/1000).toFixed(2)} 秒 {step.info}</span>{step.failure&&<><p>{step.failure.message}</p><details><summary>错误详情</summary><pre>{step.failure.detail}</pre></details></>}</div>)}</div>
    </div>
  </section>
}
