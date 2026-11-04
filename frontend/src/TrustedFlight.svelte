<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { Service } from '../bindings/github.com/openutm/utm-adapter/internal/adapter';
  import type { TrustedConfig, TrustedStatus, RegistryEntity, TrustedJob } from '../bindings/github.com/openutm/utm-adapter/internal/adapter/models';
  let { mode, onstatus }: {mode:string;onstatus:(s:TrustedStatus)=>void} = $props();
  let config = $state<TrustedConfig>();
  let status = $state<TrustedStatus>();
  let busy = $state(false), error = $state(''), notice = $state('');
  let system = $state(1), pilot = $state(''), plan = $state(''), operation = $state('');
  let plans = $state<RegistryEntity[]>([]), operations = $state<RegistryEntity[]>([]), certificates = $state<RegistryEntity[]>([]);
  let jobs = $state<TrustedJob[]>([]);
  let localPlan = $state(''), component = $state(''), keyPath = $state('trusted_flight/public_key.pem'), logPath = $state('trusted_flight/flight_log.json');
  const localTime = (d:Date) => new Date(d.getTime()-d.getTimezoneOffset()*60000).toISOString().slice(0,16);
  let start = $state(localTime(new Date(Date.now()+300000))), end = $state(localTime(new Date(Date.now()+3900000)));
  let polling = false;
  async function refresh() {
    if(polling)return;polling=true;
    try { status=await Service.GetTrustedStatus();onstatus(status); }
    catch(e){error=String(e)}finally{polling=false}
  }
  async function action(fn:()=>Promise<unknown>,message='Completed') {
    busy=true;error='';notice='';
    try{await fn();notice=message;await refresh()}catch(e){error=String(e)}finally{busy=false}
  }
  async function choices(){plans=(await Service.GetTrustedPlans())||[];operations=(await Service.GetTrustedOperations())||[];jobs=(await Service.GetTrustedJobs())||[]}
  function name(item:RegistryEntity){
    const d=item.data as unknown;
    // Wails serializes json.RawMessage as parsed JSON.
    const raw=typeof d==='string'?JSON.parse(d):d;
    return (raw as {name?:string;first_name?:string})?.name||(raw as {first_name?:string})?.first_name||item.id;
  }
  $effect(()=>{if(mode==='fleet')untrack(()=>{void action(choices,'Plans and operations loaded')})});
  onMount(()=>{Service.GetTrustedConfig().then(c=>config=c).catch(e=>error=String(e));refresh();const timer=setInterval(refresh,1000);return()=>clearInterval(timer)});
</script>

{#if mode !== 'hidden' && (mode === 'setup' || status?.enabled || mode === 'fleet')}
<section class="panel trusted" aria-label="Aerobridge Trusted Flight">
  <h2>Aerobridge Trusted Flight</h2>
  {#if error}<p role="alert" class="failure">{error}</p>{/if}
  {#if notice}<p role="status">{notice}</p>{/if}
  <div class="links"><p><b>Drone Connection Status:</b> {status?.drone?.state || 'idle'} · {status?.drone?.detail || 'Waiting for handshake'}</p><p><b>Aerobridge Connection Status:</b> {status?.server?.state || 'idle'} · {status?.server?.detail || 'Waiting for handshake'}</p></div>
  <p>{status?.detail}</p>
  {#if mode === 'setup' && config}
    <form onsubmit={e=>{e.preventDefault();action(async()=>{await Service.ApplyTrustedConfig(config!);config!.flight_passport.client_secret=''},'Trusted Flight settings saved')}}>
      <label class="toggle"><input type="checkbox" bind:checked={config.enabled}/>Enable Aerobridge Trusted Flight</label>
      {#if config.enabled}
      <label>Aerobridge PRO server URL<input required bind:value={config.server_url}/></label>
      <div class="grid"><label>Flight Passport URL<input required bind:value={config.flight_passport.url}/></label><label>Passport token endpoint<input required bind:value={config.flight_passport.token_url}/></label><label>Passport domain<input bind:value={config.flight_passport.domain}/></label><label>Passport audience<input required bind:value={config.flight_passport.audience}/></label><label>Passport client ID<input required bind:value={config.flight_passport.client_id}/></label><label>Passport client secret<input type="password" autocomplete="off" bind:value={config.flight_passport.client_secret} placeholder="Leave blank to keep saved secret"/></label><label>Organization UUID<input required bind:value={config.credentials.organization_id}/></label><label>Operator UUID<input required bind:value={config.credentials.operator_id}/></label><label>Default pilot UUID<input bind:value={config.credentials.default_pilot_id}/></label><label>Request timeout (seconds)<input type="number" min="1" max="300" bind:value={config.timeout_seconds}/></label></div>
      <label>GCS plan watch directory<input bind:value={config.watch_directory}/></label>
      <p>Stable .plan files are uploaded after the handshake. Each new plan creates a one-hour operation starting five minutes after discovery. Review the operation before requesting permission and loading it.</p>
      <label><input type="checkbox" bind:checked={config.auto_connect_drone}/>Automatically identify the sole connected drone</label>
      <label><input type="checkbox" bind:checked={config.auto_connect_server}/>Automatically authenticate and verify with Aerobridge</label>
      <p>Automatic handshake requires both options. The configured active default pilot is selected automatically.</p>
      {/if}
      <button disabled={busy}>Save Trusted Flight settings</button>
    </form>
    {#if status?.enabled}
    <div class="grid"><label>Trusted vehicle system ID<input type="number" min="1" max="255" bind:value={system}/></label><button disabled={busy} onclick={()=>action(()=>Service.ConnectTrusted(system),'Vehicle and server verified; select a pilot')}>Connect and verify vehicle</button></div>
    <label>Active Trusted Flight pilot<select bind:value={pilot}><option value="">Choose a pilot</option>{#each status?.pilots||[] as p}<option value={p.id}>{name(p)} · {p.id}</option>{/each}</select></label>
    <button disabled={busy||!pilot} onclick={()=>action(()=>Service.SelectTrustedPilot(pilot),'Fleet Management unlocked')}>Assign pilot</button>
    {/if}
  {:else if mode === 'fleet'}
    {#if status?.ready}
    <p>Aircraft {status.aircraftID} · Pilot {status.pilotID}</p>
    <button disabled={busy} onclick={()=>action(choices,'Plans and operations refreshed')}>Refresh plans and operations</button>
    <div class="grid"><label>Operation start<input type="datetime-local" bind:value={start}/></label><label>Operation end<input type="datetime-local" bind:value={end}/></label></div>
    <label>Server flight plan<select bind:value={plan}><option value="">Choose a stored plan</option>{#each plans as p}<option value={p.id}>{name(p)} · {p.id}</option>{/each}</select></label>
    <button disabled={busy||!plan} onclick={()=>action(async()=>{const job=await Service.CreateTrustedOperation(plan,'Trusted Flight',new Date(start).toISOString(),new Date(end).toISOString());operation=job.operationID;await choices()},'Operation created')}>Create operation for this aircraft and pilot</button>
    <label>Local QGroundControl .plan path<input bind:value={localPlan}/></label>
    <button disabled={busy||!localPlan} onclick={()=>action(async()=>{const job=await Service.ImportTrustedPlan(localPlan,new Date(start).toISOString(),new Date(end).toISOString());operation=job.operationID;await choices()},'Plan uploaded and operation created')}>Import plan and create operation</button>
    <label>Flight operation<select bind:value={operation}><option value="">Choose an operation</option>{#each operations as op}<option value={op.id}>{name(op)} · {op.id}</option>{/each}</select></label>
    <button disabled={busy||!operation} onclick={()=>action(()=>Service.LoadTrustedOperation(operation),'Permission granted; plan, geo-cage and token verified on vehicle')}>Request permission and load vehicle</button>
    <p>Denied or pending permission prevents transfer. Plans are read only; saving a local revision creates a new plan. Loading requires a disarmed vehicle and bidirectional MAVLink.</p>
    <h3>Vehicle credentials</h3>
    <p>Generate the key pair using the vehicle firmware's supported procedure. Only the public key is registered. PEM and DER formats are supported; enter the path exported by your firmware.</p>
    <label>Vehicle public key path<input bind:value={keyPath}/></label>
    <button disabled={busy} onclick={()=>action(()=>Service.RegisterTrustedPublicKey(keyPath),'Vehicle public key registered')}>Register vehicle public key</button>
    <button disabled={busy} onclick={()=>action(async()=>{certificates=(await Service.GetTrustedCertificates())||[]},'Signed certificates retrieved')}>Retrieve signed certificates</button>
    {#each certificates as cert}<details><summary>{cert.id}</summary><pre>{JSON.stringify(cert.data,null,2)}</pre></details>{/each}
    <label>Aerobridge component ID<input bind:value={component}/></label><button disabled={busy||!component} onclick={()=>action(()=>Service.VerifyTrustedComponent(component),'Component authenticity verified')}>Verify component</button>
    <h3>Completed flight logs</h3>
    {#each jobs as job}<p>Log {job.logID} · {job.stage || 'Awaiting signing'}</p>{#if job.stage !== 'signed'}<button disabled={busy} onclick={()=>action(async()=>{await Service.RetryTrustedLog(job.logID);await choices()},'Flight log signed')}>Retry signing {job.logID}</button>{/if}{/each}
    <label>Vehicle raw JSON log path<input bind:value={logPath}/></label>
    <button disabled={busy||!operation} onclick={()=>action(async()=>{try{await Service.CollectTrustedLog(operation,logPath)}finally{try{jobs=(await Service.GetTrustedJobs())||[]}catch{}}},'Raw log uploaded and signed by Aerobridge')}>Collect, upload and sign flight log</button>
    {:else}<p>Fleet Management is locked. Complete the handshake in Connection settings.</p>{/if}
  {/if}
</section>
{/if}
<style>
.trusted{padding:28px;margin:24px 0;background:white;border:1px solid #dce3e0;border-radius:12px}.links,.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(240px,1fr));gap:16px}label{display:flex;flex-direction:column;gap:6px;margin:14px 0}input,select{padding:10px;border:1px solid #b7c6bf;border-radius:6px;font:inherit}input[type=checkbox]{width:18px}button{padding:10px 16px;margin:8px 8px 8px 0;border:1px solid #176b51;border-radius:6px;background:#176b51;color:white;cursor:pointer}button:disabled{opacity:.45;cursor:default}.toggle{flex-direction:row;align-items:center}.failure{color:#ad302f}p{line-height:1.5}pre{overflow:auto;max-height:240px}
</style>
