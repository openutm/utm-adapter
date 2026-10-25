<script lang="ts">
  import { onMount } from 'svelte';
  import { Service } from '../bindings/github.com/openutm/utm-adapter/internal/adapter';
  import type { Config, Snapshot, Link, Assignment, Fleet } from '../bindings/github.com/openutm/utm-adapter/internal/adapter/models';

  let snapshot = $state<Snapshot | null>(null);
  let form = $state<Config>({} as Config);
  let page = $state('status');
  let assignment = $state<Assignment>({systemID:1, declarationID:'', vehicleID:'', operatorID:'', pilot:{id:'', name:'', license:'', contact:''}, aerobridgeOperationID:''} as Assignment);
  let fleet = $state<Fleet | null>(null);
  let declarations = $state<string[]>([]);
  async function loadChoices() { declarations = ((await Service.GetDeclarations()) || []).map(d => d.id); if (snapshot?.config.aerobridgeEnabled) fleet = await Service.GetFleet(); }

  let busy = $state(false);
  let error = $state('');
  let notice = $state('');
  let loaded = false;
  let polling = false;
  const healthy = (link?: Link) => link?.state === 'connected';
  const label = (link?: Link) => ({connected:'Connected', waiting:'Waiting', error:'Needs attention', idle:'Paused'}[link?.state || 'idle'] || 'Checking');
  const number = (v: number | null | undefined, decimals = 1) => v == null ? '—' : v.toFixed(decimals);
  const age = (date: string) => date.startsWith('0001') ? 'No sample' : `${Math.max(0, Math.floor((Date.now() - Date.parse(date)) / 1000))}s ago`;
  let live = $derived.by(() => (snapshot?.running ? snapshot.vehicles : [])?.filter(v => Date.parse(snapshot!.now) - Date.parse(v.lastSeen) < 5000) || []);
  let detected = $derived.by(() => snapshot?.gcs?.filter(g => g.running || g.installed) || []);
  let planner = $derived(snapshot?.gcs?.find(g => g.id === 'mission-planner'));
  let qgc = $derived(snapshot?.gcs?.find(g => g.id === 'qgroundcontrol'));
  let title = $derived.by(() => !snapshot?.running ? 'Ready when you are.' : healthy(snapshot?.telemetry) && healthy(snapshot?.access) ? 'Your connections are ready.' : 'Let’s get you connected.');

  async function refresh() {
    if (polling) return;
    polling = true;
    try {
      snapshot = await Service.GetSnapshot();
      if (!loaded) { form = {...snapshot.config}; loaded = true; }
    } catch (e) { error = String(e); }
    finally { polling = false; }
  }
  async function action(fn: () => Promise<unknown>, success = '') {
    busy = true; error = ''; notice = '';
    try { await fn(); await refresh(); if (success) notice = success; }
    catch (e) { error = String(e); }
    finally { busy = false; }
  }
  async function save() {
    await action(() => Service.ApplyConfig(form), 'Settings saved. Connections are running.');
    form.token = ''; form.clientSecret = ''; form.aerobridgeToken = '';
  }
  async function setGCSOptions(automatic: boolean, plugin: boolean) {
    await action(async () => {
      const current = await Service.GetSnapshot();
      await Service.ApplyConfig({...current.config, source: plugin && current.config.source === 'qgroundcontrol' ? 'auto' : current.config.source, autoConfigureGCS: automatic, missionPlannerPlugin: plugin});
    }, plugin ? 'Plugin selected. Close Mission Planner to complete installation, then reopen it.' : 'Ground station preference saved.');
    if (snapshot) { form.autoConfigureGCS = snapshot.config.autoConfigureGCS; form.missionPlannerPlugin = snapshot.config.missionPlannerPlugin; form.source = snapshot.config.source; }
  }
  async function removePlugin() {
    await action(() => Service.RemoveMissionPlannerPlugin(), 'Plugin removed and backed up. Mission Planner can now be reopened.');
    if (snapshot) form.missionPlannerPlugin = snapshot.config.missionPlannerPlugin;
  }
  onMount(() => {
    refresh(); const interval = setInterval(refresh, 1000);
    return () => clearInterval(interval);
  });
</script>

<svelte:head><title>UTM Adapter</title></svelte:head>
<div class="shell">
  <header>
    <div class="brand"><span class="logo">⊕</span><div><strong>UTM Adapter</strong><span>OPENUTM</span></div></div>
    <span class="version">VERSION <b>{snapshot?.version || '0.2.0'}</b></span>
  </header>
  <nav aria-label="Main navigation">
    <div class="tabs"><button class:active={page === 'status'} onclick={() => page = 'status'}>Overview</button><button class:active={page === 'sessions'} onclick={() => {page = 'sessions'; action(loadChoices);}}>Flight sessions</button><button class:active={page === 'gcs'} onclick={() => page = 'gcs'}>Ground stations</button><button class:active={page === 'settings'} onclick={() => page = 'settings'}>Connection settings</button></div>
    <span class="background"><i class:running={snapshot?.running}></i>{snapshot?.running ? 'Running in background' : 'Connections paused'}</span>
  </nav>

  <main>
    {#if error}<div class="alert error" role="alert">{error}<button aria-label="Dismiss error" onclick={() => error = ''}>×</button></div>{/if}
    {#if notice}<div class="alert success" role="status">{notice}<button aria-label="Dismiss notification" onclick={() => notice = ''}>×</button></div>{/if}
    {#if !snapshot}<div class="intro"><h1>Starting UTM Adapter…</h1><p>Loading your connections.</p></div>
    {:else if page === 'status'}
      <section class="intro"><div><div class="eyebrow">CONNECTION OVERVIEW</div><h1>{title}</h1><p>A quiet link between your ground station and Flight Blender.</p></div><button class="secondary" disabled={busy} onclick={() => action(() => snapshot?.running ? Service.Stop() : Service.Start())}>{snapshot.running ? 'Pause connections' : 'Resume connections'}</button></section>
      <section class="cards" aria-label="Connection status">
        <article class="card"><div class="card-top"><span class="card-icon">⌘</span><span class:good={detected.some(g => g.running)} class="badge">{detected.some(g => g.running) ? 'Running' : detected.length ? 'Found' : 'Searching'}</span></div><h2>Ground station</h2><strong>{snapshot.config.source === 'direct' ? 'Direct MAVLink' : detected.length ? detected.map(g => g.name).join(' · ') : 'Automatic discovery'}</strong><p>{snapshot.config.source === 'direct' ? 'Listening to your configured vehicle endpoint.' : detected.length ? detected.map(g => g.setupDetail || 'Checking forwarding settings…').join(' · ') : 'Open QGroundControl or Mission Planner, or choose direct MAVLink.'}</p></article>
        <article class="card"><div class="card-top"><span class="card-icon">◎</span><span class:good={healthy(snapshot.access)} class="badge">{label(snapshot.access)}</span></div><h2>Flight Blender</h2><strong>{healthy(snapshot.blender) ? 'Server reachable' : 'Awaiting connection'}</strong><p>{healthy(snapshot.blender) ? snapshot.access.detail : snapshot.blender.detail}</p><span class="mono endpoint">{snapshot.config.baseURL}</span></article>
        <article class="card"><div class="card-top"><span class="card-icon">↝</span><span class:good={healthy(snapshot.telemetry)} class="badge">{label(snapshot.telemetry)}</span></div><h2>Telemetry stream</h2><strong>{live.length ? `${live.length} vehicle${live.length === 1 ? '' : 's'} connected` : 'No live vehicle yet'}</strong><p>{snapshot.telemetry.detail}</p><span class="mono endpoint">{snapshot.frames.toLocaleString()} frames · {snapshot.parseErrors} parse errors</span>{#if snapshot.lastParseError}<p>Last parse error: {snapshot.lastParseError}</p>{/if}</article>
      </section>

      {#if !healthy(snapshot.telemetry) && snapshot.running}
        <section class="setup"><span class="setup-icon">↗</span><div><h3>{snapshot.config.source === 'direct' ? 'Connect your MAVLink source' : 'Your ground station connects automatically'}</h3>
          {#if snapshot.config.source === 'direct'}<p>Send vehicle telemetry to <code>{snapshot.config.address}</code>, or select a TCP or serial connection in settings.</p>
          {:else if detected.length}{#each detected as g}<p><b>{g.name}:</b> {g.setupDetail || 'Checking forwarding settings…'}</p>{/each}
          {:else}<p>Open QGroundControl or Mission Planner once, then close it. The adapter will find it and set up local telemetry forwarding.</p>{/if}
          {#if snapshot.config.source !== 'direct'}<p class="muted">Settings are updated while the GCS is closed. Open it afterward and connect your vehicle normally. Existing forwarding destinations are preserved.</p>{/if}
          <button class="text-button" onclick={() => page = snapshot?.config.source === 'direct' ? 'settings' : 'gcs'}>{snapshot.config.source === 'direct' ? 'Connection settings' : 'Ground station options'} <span>→</span></button></div></section>
      {/if}

      <section class="panel"><div class="panel-heading"><div><h2>Vehicle telemetry</h2><p>Live values from the incoming MAVLink stream</p></div><span class="quiet">Updates every second</span></div>
        {#if !snapshot.vehicles?.length}<div class="empty"><span>⌁</span><strong>Waiting for the first vehicle</strong><p>GPS, altitude and speed will appear here when telemetry arrives.</p></div>
        {:else}{#each snapshot.vehicles as vehicle}<div class="vehicle"><div class="vehicle-heading"><strong>Vehicle {vehicle.systemID}</strong><span class="badge" class:good={live.some(v => v.systemID === vehicle.systemID)}>{age(vehicle.lastSeen)}</span><span class="quiet">{vehicle.armed ? 'Armed' : 'Disarmed'}</span></div><div class="metrics"><div><span>POSITION</span><strong class="coordinates">{number(vehicle.latitude,6)}, {number(vehicle.longitude,6)}</strong></div><div><span>ALTITUDE · MSL</span><strong>{number(vehicle.altitudeMSL)} <small>m</small></strong></div><div><span>GROUND SPEED</span><strong>{number(vehicle.speed)} <small>m/s</small></strong></div><div><span>GPS</span><strong>{vehicle.fix >= 3 ? '3D fix' : 'No 3D fix'} <small>· {vehicle.satellites === 255 ? '?' : vehicle.satellites} sats</small></strong></div></div><p class="quiet">WGS84 altitude: {number(vehicle.altitudeWGS84)} m · Relative altitude: {number(vehicle.relativeAltitude)} m · GPS {age(vehicle.gpsAt)}</p></div>{/each}{/if}
      </section>
      <section class="delivery"><div><h3>Flight Blender telemetry delivery</h3><p>{(snapshot.config.publish || snapshot.config.sessionDelivery) ? snapshot.delivery.detail : 'Off. Enable sending for an existing declaration in connection settings.'}</p></div><span class="badge" class:good={snapshot.sent > 0}>{snapshot.sent} accepted</span></section>
      <details class="activity"><summary>Recent activity <span>{snapshot.events?.length ?? 0}</span></summary>{#each snapshot.events?.slice(0,8) || [] as event}<div><time>{new Date(event.at).toLocaleTimeString()}</time><span>{event.message}</span></div>{/each}<p class="quiet config-path">Configuration: {snapshot.configPath}</p></details>
    {:else if page === 'sessions'}
      <section class="intro"><div><div class="eyebrow">FLIGHT RECORDS</div><h1>Pilot, aircraft, declaration.</h1><p>Assign each MAVLink system before flying. Records remain on this computer during an outage.</p></div><button class="secondary" disabled={busy} onclick={() => action(() => Service.Synchronize(), 'Synchronization attempt completed; check delivery status below.')}>Synchronize now</button></section>
      <section class="panel settings"><h2>Storage and delivery</h2><button class="secondary" disabled={busy} onclick={() => action(loadChoices, 'Declaration and fleet choices refreshed.')}>Refresh choices</button><p>{snapshot.records.detail}</p><p>Restrictions: {snapshot.restrictions.detail || 'Waiting for refresh'}</p><p>Aerobridge: {snapshot.aerobridge.detail || 'Disabled'}</p><p class="help">GCS map feed: {snapshot.bridgeURL || 'Paused'}/v1/restrictions</p></section>
      <form onsubmit={(e) => {e.preventDefault(); action(() => Service.BeginSession(assignment), 'Session started. Telemetry is being recorded locally.');}}>
        <section class="panel settings"><h2>Start a session</h2>
          <div class="form-grid"><label>MAVLink system ID<input type="number" min="1" max="255" required bind:value={assignment.systemID}/></label><label>Declaration UUID<input required bind:value={assignment.declarationID} list="declarations"/><datalist id="declarations">{#each declarations as id}<option value={id}></option>{/each}</datalist></label></div>
          <div class="form-grid"><label>Aircraft ID<input required bind:value={assignment.vehicleID} list="aircraft"/><datalist id="aircraft">{#each fleet?.aircraft || [] as aircraft}<option value={aircraft.id}></option>{/each}</datalist></label><label>Operator ID<input required bind:value={assignment.operatorID}/></label></div>
          <div class="form-grid"><label>Pilot ID<input bind:value={assignment.pilot.id} list="pilots"/><datalist id="pilots">{#each fleet?.pilots || [] as pilot}<option value={pilot.id}></option>{/each}</datalist></label><label>Pilot name<input required bind:value={assignment.pilot.name}/></label></div>
          <div class="form-grid"><label>Pilot licence<input required bind:value={assignment.pilot.license}/></label><label>Pilot contact<input required bind:value={assignment.pilot.contact}/></label></div>
          {#if snapshot.config.aerobridgeEnabled}<label>Aerobridge operation UUID<input required bind:value={assignment.aerobridgeOperationID} list="operations"/><datalist id="operations">{#each fleet?.operations || [] as operation}<option value={operation.id}></option>{/each}</datalist></label>{/if}
          <p class="help">Pilot details stay attached to this session. Choose system filter 0 in settings to receive multiple aircraft. Live delivery requires “Send session telemetry” below; audit synchronization runs independently.</p>
          <button class="primary" disabled={busy}>Start recording</button>
        </section>
      </form>
      <section class="panel settings"><h2>Session history</h2>
        {#each snapshot.sessions || [] as record}<article class="vehicle"><h3>{record.session.assignment.vehicleID} · system {record.session.assignment.systemID}</h3><p>{record.session.assignment.pilot.name} · {record.samples} samples · {record.session.endedAt ? 'Ended' : 'Recording'}</p><p>Flight Blender: {record.blenderPending ? 'Queued' : 'Synchronized'} · Aerobridge: {!record.session.aerobridgeURL ? 'Disabled' : record.aerobridgePending ? 'Queued' : 'Synchronized'}</p>
          {#if !record.session.endedAt}<button class="secondary" disabled={busy} onclick={() => action(() => Service.EndSession(record.session.id), 'Session ended. The final record is queued for synchronization.')}>End session</button>{:else}<button class="secondary" disabled={busy} onclick={() => action(async () => {const path = await Service.ExportSession(record.session.id); notice = `Report saved: ${path}`;})}>Export JSON report</button>{/if}
        </article>{/each}
      </section>
    {:else if page === 'gcs'}
      <section class="intro"><div><div class="eyebrow">GROUND STATIONS</div><h1>Your GCS. Your choice.</h1><p>Connect your existing ground station or add an optional OpenUTM enhancement.</p></div></section>
      <section class="panel settings"><div class="panel-heading"><div><h2>Connect your existing GCS</h2><p>QGroundControl forwarding can be configured automatically.</p></div><span class="badge" class:good={snapshot.config.autoConfigureGCS}>{snapshot.config.autoConfigureGCS ? 'Automatic' : 'Manual'}</span></div>
        <div class="found">{#each snapshot.gcs as g}<span><i class:running={g.running}></i>{g.name} <b>{g.running ? 'running' : g.installed ? 'installed' : 'not found'}</b></span>{/each}</div>
        <p class="help">{qgc?.setupDetail || 'Open QGroundControl once so we can find its settings.'}</p>
        <p class="help">Mission Planner can use its built-in MAVLink mirror: Ctrl+F → Mavlink → UDP client, forwarding to <code>{snapshot.config.address}</code>, with write-to-vehicle disabled. The optional plugin below starts forwarding on launch.</p>
        <button type="button" class="secondary" disabled={busy} onclick={() => setGCSOptions(!snapshot!.config.autoConfigureGCS, snapshot!.config.missionPlannerPlugin)}>{snapshot.config.autoConfigureGCS ? 'Use manual configuration' : 'Enable automatic configuration'}</button>
        <p class="help">Turning automatic configuration off keeps existing forwarding and installed plugins in place. Direct MAVLink mode skips GCS configuration.</p>
      </section>
      <section class="panel settings"><div class="panel-heading"><div><h2>OpenUTM for Mission Planner</h2><p>Telemetry forwarding and airspace map overlays · version 0.2.0</p></div><span class="badge" class:good={planner?.enhancementState === 'installed'}>{planner?.enhancementState === 'installed' ? 'File installed' : snapshot.config.missionPlannerPlugin ? 'Selected' : 'Optional'}</span></div>
        <p>Forward telemetry from connected aircraft and show Flight Blender restrictions on the Flight Data map. Flight Blender credentials stay in the adapter.</p>
        <p class="help">Installs one plugin file into your existing Mission Planner. Close Mission Planner to install, update or remove it, then reopen it. Changed files are backed up. Windows runtime validation is still pending.</p>
        {#if planner?.installed}<p class="help">{planner.setupDetail}</p>{:else}<p class="help">Mission Planner has not been found on this computer. This option becomes available when its installation is detected.</p>{/if}
        {#if planner?.enhancementState === 'installed'}
          <button class="secondary" type="button" disabled={busy || planner.running} onclick={removePlugin}>Remove OpenUTM plugin</button>
          {#if planner.running}<p class="help">Close Mission Planner to remove the plugin.</p>{/if}
          {#if !snapshot.config.missionPlannerPlugin}<button class="text-button" disabled={busy || snapshot.config.source === 'direct'} onclick={() => setGCSOptions(true,true)}>Manage plugin updates automatically</button>{/if}
        {:else if snapshot.config.missionPlannerPlugin}
          <button class="secondary" type="button" disabled={busy} onclick={() => setGCSOptions(snapshot!.config.autoConfigureGCS,false)}>Cancel automatic plugin installation</button>
        {:else}
          <button class="primary" type="button" disabled={busy || !planner?.installed || snapshot.config.source === 'direct' || snapshot.config.transport !== 'udp'} onclick={() => setGCSOptions(true,true)}>Install OpenUTM plugin</button>
        {/if}
        {#if snapshot.config.transport !== 'udp'}<p class="help">Choose UDP in Connection settings to use local GCS forwarding.</p>{/if}
        {#if snapshot.config.source === 'direct'}<p class="help">Select Automatic or Mission Planner as your telemetry source in Connection settings to use the plugin.</p>{/if}
        {#if planner?.settingsPath}<details class="activity"><summary>Installation details</summary><p class="help config-path">Plugin: {planner.settingsPath}</p>{#if planner.backupPath}<p class="help config-path">Backup: {planner.backupPath}</p>{/if}<p class="help">“File installed” confirms the plugin is on disk. Incoming telemetry confirms that forwarding is working.</p></details>{/if}
      </section>
      <section class="panel settings"><div class="panel-heading"><div><h2>OpenUTM QGroundControl</h2><p>Optional custom application</p></div><span class="badge">Source integration</span></div>
        <p>The OpenUTM QGC build draws restriction boundaries from the local adapter feed. Download it from the release's GCS packages, or build it using DEVELOPING.md.</p>
        <p class="help">Existing QGroundControl supports telemetry through automatic forwarding. Install the OpenUTM build to add native map overlays.</p>
      </section>
    {:else}
      <section class="intro"><div><div class="eyebrow">CONNECTION SETTINGS</div><h1>Set up once. Stay connected.</h1><p>Your adapter keeps working when you close this window.</p></div></section>
      <form onsubmit={(e) => {e.preventDefault(); save();}}>
        <section class="panel settings"><div class="section-title"><span>01</span><div><h2>Flight Blender</h2><p>Connect to your UTM service</p></div></div>
          <label>Server URL<input bind:value={form.baseURL} placeholder="http://127.0.0.1:8000" required /></label>
          <label>Authentication<select bind:value={form.authMode}><option value="bearer">Bearer token</option><option value="oauth">OAuth · client credentials</option><option value="none">None · local development server</option></select></label>
          {#if form.authMode === 'bearer'}<label>Bearer token<input type="password" autocomplete="off" bind:value={form.token} placeholder={snapshot.config.hasToken ? 'Saved securely · leave blank to keep' : 'Paste access token'} /></label>{/if}
          {#if form.authMode === 'oauth'}<label>Token URL<input bind:value={form.tokenURL} placeholder="https://auth.example.com/oauth/token" required /></label><div class="form-grid"><label>Client ID<input bind:value={form.clientID} required /></label><label>Client secret<input type="password" autocomplete="off" bind:value={form.clientSecret} placeholder={snapshot.config.hasClientSecret ? 'Saved securely · leave blank to keep' : 'Client secret'} /></label></div><label>Scopes<input bind:value={form.scope} /></label>{/if}
          <p class="help">Credentials stay in your OS credential store. The connection check verifies read access; telemetry sending also needs <code>flightblender.write</code>.</p>
          {#if snapshot.config.hasToken || snapshot.config.hasClientSecret}<button type="button" class="text-button danger" disabled={busy} onclick={() => action(() => Service.ClearCredentials(), 'Credentials removed. Connections paused.')}>Remove saved credentials</button>{/if}
        </section>
        <section class="panel settings"><h2>Session delivery and optional fleet registry</h2>
          <label class="toggle"><input type="checkbox" bind:checked={form.sessionDelivery}/><span>Send session telemetry to Flight Blender</span></label>
          <label>Local GCS map address<input bind:value={form.bridgeAddress} placeholder="127.0.0.1:14552"/></label>
          <label class="toggle"><input type="checkbox" bind:checked={form.aerobridgeEnabled}/><span>Connect Aerobridge</span></label>
          {#if form.aerobridgeEnabled}<label>Aerobridge URL<input required bind:value={form.aerobridgeURL}/></label><label>Aerobridge bearer token<input type="password" autocomplete="off" bind:value={form.aerobridgeToken} placeholder={snapshot.config.hasAerobridgeToken ? 'Saved securely · leave blank to keep' : 'Paste access token'}/></label>{/if}
        </section>
        <section class="panel settings"><div class="section-title"><span>02</span><div><h2>Ground station & telemetry</h2><p>Use your GCS connection, or a direct MAVLink source</p></div></div>
          <label>Telemetry source<select bind:value={form.source}><option value="auto">Automatic · discover ground station</option><option value="qgroundcontrol">QGroundControl forwarding</option><option value="mission-planner">Mission Planner forwarding</option><option value="direct">Direct MAVLink</option></select></label>
          <label class="toggle"><input type="checkbox" bind:checked={form.autoConfigureGCS} /><span><b>Set up ground station forwarding automatically</b><small>Updates QGC forwarding while the GCS is closed. Mission Planner plugin installation is optional in Ground stations.</small></span></label>
          <button class="text-button" type="button" onclick={() => page = 'gcs'}>Ground station enhancements & options →</button>
          <div class="found">{#each snapshot.gcs as g}<span><i class:running={g.running}></i>{g.name} <b>{g.running ? 'running' : g.installed ? 'installed' : 'not found'}</b></span>{/each}</div>
          <div class="form-grid"><label>Connection type<select bind:value={form.transport}><option value="udp">UDP · listen for forwarded telemetry</option><option value="tcp">TCP · connect to endpoint</option><option value="serial">Serial · connect to device</option></select></label>{#if form.transport !== 'serial'}<label>{form.transport === 'udp' ? 'Listen address' : 'Remote address'}<input bind:value={form.address} placeholder={form.transport === 'udp' ? '127.0.0.1:14551' : '127.0.0.1:5760'} required /></label>{/if}</div>
          {#if form.transport === 'serial'}<div class="form-grid"><label>Device<input bind:value={form.serialDevice} placeholder="/dev/cu.usbmodem… or COM3" required /></label><label>Baud rate<input type="number" bind:value={form.baud} min="1200" required /></label></div>{/if}
          {#each detected as g}<p class="help"><b>{g.name}:</b> {g.setupDetail || 'Checking forwarding settings…'}{#if g.settingsPath}<br /><span class="config-path">Settings: {g.settingsPath}</span>{/if}{#if g.backupPath}<br /><span class="config-path">Backup: {g.backupPath}</span>{/if}</p>{/each}
          <p class="help">Automatic setup uses local UDP forwarding. Close the GCS, save these settings, then reopen it once forwarding is ready. Direct mode receives from a vehicle, router or SITL without changing GCS settings. No vehicle commands are sent.</p>
          <label>MAVLink system ID<input type="number" min="0" max="255" bind:value={form.systemID} required /><small>0 discovers all vehicle systems. Choose a specific system before sending telemetry.</small></label>
        </section>
        <section class="panel settings"><div class="section-title"><span>03</span><div><h2>Send telemetry to Flight Blender</h2><p>Optional · use an existing flight declaration for this first version</p></div></div>
          <label class="toggle"><input type="checkbox" bind:checked={form.publish} /><span><b>Enable telemetry delivery</b><small>Sends fresh GPS samples about once per second while connected</small></span></label>
          <label>Flight declaration ID<input bind:value={form.declarationID} placeholder="Existing declaration UUID" required={form.publish} /></label>
          <div class="form-grid"><label>Vehicle serial number<input bind:value={form.vehicleID} placeholder="Your registered vehicle ID" required={form.publish} /></label><label>Operator ID<input bind:value={form.operatorID} required={form.publish} /></label></div>
          <p class="help">The declaration must be active now and in a state that accepts telemetry. Sending requires a fresh vehicle heartbeat and 3D GPS fix, including MAVLink 2 WGS84 ellipsoid altitude, speed and course. MSL altitude is shown locally and is never substituted for WGS84 altitude.</p>
        </section>
        <div class="save-bar"><p>Settings save locally as <code>product.json</code>.</p><button class="primary" disabled={busy} type="submit">{busy ? 'Connecting…' : 'Save & connect'} <span>→</span></button></div>
      </form>
    {/if}
  </main>
  <footer><span><i class="running"></i>Close this window to keep the adapter in your tray.</span><span>OpenUTM · 0.2.0</span></footer>
</div>
